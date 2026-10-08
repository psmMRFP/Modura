package delivery

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

// SMTP sends via implicit TLS only, with certificate validation and bounded IO.
type SMTP struct{ address, host, username, password, from, origin string }

// NewSMTP validates deployment-provided values without echoing credentials.
func NewSMTP(address, username, password, from, origin string) (*SMTP, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" || username == "" || password == "" {
		return nil, errors.New("invalid SMTP configuration")
	}
	sender, err := mail.ParseAddress(from)
	if err != nil || sender.Address != from {
		return nil, errors.New("invalid SMTP sender")
	}
	base, err := url.Parse(origin)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("public origin must be an HTTPS origin")
	}
	return &SMTP{address: address, host: host, username: username, password: password, from: from, origin: strings.TrimRight(origin, "/")}, nil
}

// Send returns only a redacted failure, never SMTP server diagnostics or message text.
func (s *SMTP) Send(ctx context.Context, message identity.PublicMail) (result error) {
	defer func() {
		if result != nil {
			result = errors.New("SMTP delivery failed")
		}
	}()
	parsed, err := mail.ParseAddress(message.Email)
	if err != nil || parsed.Address != message.Email || strings.ContainsAny(message.Code, "\r\n") || message.Code == "" {
		return errors.New("invalid message")
	}
	path, subject := "verify-email", "Verify your WhereToLive email"
	if message.Purpose == "password_reset" {
		path, subject = "reset-password", "Reset your WhereToLive password"
	} else if message.Purpose != "email_verification" {
		return errors.New("invalid message purpose")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 8 * time.Second}, Config: &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}}
	connection, err := dialer.DialContext(ctx, "tcp", s.address)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	deadline, _ := ctx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	client, err := smtp.NewClient(connection, s.host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if err = client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
		return err
	}
	if err = client.Mail(s.from); err != nil {
		return err
	}
	if err = client.Rcpt(message.Email); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOpen %s/en/%s and enter this one-time code:\r\n\r\n%s\r\n\r\nIf you did not request this email, you can ignore it.\r\n", s.from, message.Email, subject, s.origin, path, message.Code)
	if _, err = io.WriteString(writer, body); err != nil {
		_ = writer.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
