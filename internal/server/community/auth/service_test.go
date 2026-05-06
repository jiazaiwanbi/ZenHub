package auth

import (
	"errors"
	"testing"
	"time"
)

func TestServiceLoginAndVerify(t *testing.T) {
	service, err := New("admin", "secret-pass", "0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Unix(1_700_000_000, 0).UTC()
	service.now = func() time.Time { return now }

	session, err := service.Login("admin", "secret-pass")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.TokenType != "Bearer" || session.AccessToken == "" {
		t.Fatalf("session = %#v", session)
	}

	claims, err := service.Verify(session.AccessToken)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.Username != "admin" {
		t.Fatalf("claims.Username = %q, want admin", claims.Username)
	}

	if _, err := service.Login("admin", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}

	service.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := service.Verify(session.AccessToken); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Verify() after expiration error = %v, want ErrExpiredToken", err)
	}
}
