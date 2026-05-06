package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid bearer token")
	ErrExpiredToken       = errors.New("bearer token expired")
)

type Claims struct {
	Username  string
	ExpiresAt time.Time
}

type Session struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	ExpiresAt   int64  `json:"expires_at"`
	Username    string `json:"username"`
}

type Service struct {
	username string
	password string
	secret   []byte
	ttl      time.Duration
	now      func() time.Time
}

type tokenPayload struct {
	Username string `json:"sub"`
	Expires  int64  `json:"exp"`
}

func New(username, password, secret string, ttl time.Duration) (*Service, error) {
	if strings.TrimSpace(username) == "" {
		return nil, errors.New("auth username is required")
	}
	if strings.TrimSpace(password) == "" {
		return nil, errors.New("auth password is required")
	}
	if len(strings.TrimSpace(secret)) < 16 {
		return nil, errors.New("auth secret must be at least 16 characters")
	}
	if ttl <= 0 {
		return nil, errors.New("auth token TTL must be greater than zero")
	}

	return &Service{
		username: username,
		password: password,
		secret:   []byte(secret),
		ttl:      ttl,
		now:      time.Now,
	}, nil
}

func (s *Service) Login(username, password string) (Session, error) {
	if !equalConstantTime(strings.TrimSpace(username), s.username) || !equalConstantTime(password, s.password) {
		return Session{}, ErrInvalidCredentials
	}

	expiresAt := s.now().UTC().Add(s.ttl)
	token, err := s.signToken(tokenPayload{
		Username: s.username,
		Expires:  expiresAt.Unix(),
	})
	if err != nil {
		return Session{}, err
	}

	return Session{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.ttl / time.Second),
		ExpiresAt:   expiresAt.Unix(),
		Username:    s.username,
	}, nil
}

func (s *Service) Verify(token string) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 {
		return Claims{}, ErrInvalidToken
	}

	payloadSegment := parts[0]
	signatureSegment := parts[1]
	expectedSignature := s.sign(payloadSegment)

	signature, err := base64.RawURLEncoding.DecodeString(signatureSegment)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	if !hmac.Equal(signature, expectedSignature) {
		return Claims{}, ErrInvalidToken
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadSegment)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}

	var payload tokenPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return Claims{}, ErrInvalidToken
	}

	expiresAt := time.Unix(payload.Expires, 0).UTC()
	if s.now().UTC().After(expiresAt) {
		return Claims{}, ErrExpiredToken
	}

	return Claims{
		Username:  payload.Username,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) signToken(payload tokenPayload) (string, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode token payload: %w", err)
	}

	payloadSegment := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signatureSegment := base64.RawURLEncoding.EncodeToString(s.sign(payloadSegment))
	return payloadSegment + "." + signatureSegment, nil
}

func (s *Service) sign(payload string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func equalConstantTime(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
