package identity

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAccessTokenValidation(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := []byte(strings.Repeat("k", 32))
	signer, err := NewAccessTokenSigner("wheretolive", "wheretolive-admin", "current", key, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(Actor{TenantID: "tenant", UserID: "user", SessionID: "session"}, "token", 2, now)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewAccessTokenVerifier("wheretolive", "wheretolive-admin", map[string][]byte{"current": key}, 5*time.Second)
	claims, err := verifier.Verify(token, now.Add(time.Minute))
	if err != nil || claims.TenantID != "tenant" || claims.SecurityVersion != 2 {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}
	if _, err := verifier.Verify(token, now.Add(6*time.Minute)); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("err=%v", err)
	}
	parts := strings.Split(token, ".")
	parts[1] = strings.TrimSuffix(parts[1], "A") + "A"
	if _, err := verifier.Verify(strings.Join(parts, "."), now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err=%v", err)
	}
}

func TestOpaqueToken(t *testing.T) {
	token, err := NewOpaqueToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 43 {
		t.Fatalf("token length = %d", len(token))
	}
	if HashOpaqueToken(token) == HashOpaqueToken(token+"x") {
		t.Fatal("different tokens have equal hashes")
	}
}
