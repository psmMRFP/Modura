package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"
)

// PublicIdentity holds deployment-only mail and CAPTCHA controls; disabled by default.
type PublicIdentity struct {
	Enabled                                                   bool
	EncryptionKey                                             []byte
	SMTPAddress, SMTPUsername, SMTPPassword, SMTPFrom, Origin string
	ChallengeSecret, ChallengeSiteKey, ChallengeHostname      string
	VerificationLifetime, ResetLifetime, RateWindow           time.Duration
	GlobalLimit, NetworkLimit, AccountLimit                   int32
}

func publicIdentityFromEnv() (PublicIdentity, error) {
	var p PublicIdentity
	var err error
	if p.Enabled, err = boolean("MODURA_PUBLIC_IDENTITY_ENABLED", false); err != nil {
		return p, err
	}
	if !p.Enabled {
		return p, nil
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("MODURA_IDENTITY_MAIL_KEY"))
	if err != nil || len(key) != 32 {
		return p, fmt.Errorf("MODURA_IDENTITY_MAIL_KEY must encode 32 bytes")
	}
	p.EncryptionKey = key
	p.SMTPAddress = os.Getenv("MODURA_SMTP_ADDRESS")
	p.SMTPUsername = os.Getenv("MODURA_SMTP_USERNAME")
	p.SMTPPassword = os.Getenv("MODURA_SMTP_PASSWORD")
	p.SMTPFrom = os.Getenv("MODURA_SMTP_FROM")
	p.Origin = os.Getenv("MODURA_PUBLIC_ORIGIN")
	p.ChallengeSecret = os.Getenv("MODURA_TURNSTILE_SECRET")
	p.ChallengeSiteKey = os.Getenv("MODURA_TURNSTILE_SITE_KEY")
	p.ChallengeHostname = os.Getenv("MODURA_TURNSTILE_HOSTNAME")
	host, _, err := net.SplitHostPort(p.SMTPAddress)
	if err != nil || host == "" || p.SMTPUsername == "" || p.SMTPPassword == "" {
		return p, fmt.Errorf("public identity requires TLS SMTP configuration")
	}
	sender, err := mail.ParseAddress(p.SMTPFrom)
	if err != nil || sender.Address != p.SMTPFrom {
		return p, fmt.Errorf("MODURA_SMTP_FROM must be a mailbox")
	}
	base, err := url.Parse(p.Origin)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return p, fmt.Errorf("MODURA_PUBLIC_ORIGIN must be an HTTPS origin")
	}
	if p.ChallengeSecret == "" || p.ChallengeSiteKey == "" || p.ChallengeHostname != base.Hostname() {
		return p, fmt.Errorf("public identity requires Turnstile keys and matching hostname")
	}
	// Cloudflare test keys must never enable real public enrollment.
	if strings.HasPrefix(p.ChallengeSecret, "1x000") || strings.HasPrefix(p.ChallengeSecret, "2x000") || strings.HasPrefix(p.ChallengeSecret, "3x000") || strings.Contains(p.ChallengeSiteKey, "000000000000000000") {
		return p, fmt.Errorf("public identity rejects Turnstile testing keys")
	}
	if p.VerificationLifetime, err = duration("MODURA_PUBLIC_VERIFY_LIFETIME", time.Hour); err != nil {
		return p, err
	}
	if p.ResetLifetime, err = duration("MODURA_PUBLIC_RESET_LIFETIME", 15*time.Minute); err != nil {
		return p, err
	}
	if p.RateWindow, err = duration("MODURA_PUBLIC_RATE_WINDOW", time.Hour); err != nil {
		return p, err
	}
	if p.VerificationLifetime > 24*time.Hour || p.ResetLifetime > time.Hour || p.RateWindow > 24*time.Hour {
		return p, fmt.Errorf("public identity lifetime exceeds deployment maximum")
	}
	names := []string{"MODURA_PUBLIC_GLOBAL_LIMIT", "MODURA_PUBLIC_NETWORK_LIMIT", "MODURA_PUBLIC_ACCOUNT_LIMIT"}
	defaults := []int{1000, 30, 10}
	limits := make([]int32, 3)
	for i, name := range names {
		n, err := integer(name, defaults[i])
		if err != nil || n > 100000 {
			return p, fmt.Errorf("invalid %s", name)
		}
		limits[i] = int32(n)
	}
	p.GlobalLimit, p.NetworkLimit, p.AccountLimit = limits[0], limits[1], limits[2]
	return p, nil
}
