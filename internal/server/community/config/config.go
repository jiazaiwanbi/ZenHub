package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultListen   = "127.0.0.1:8081"
	defaultTokenTTL = 24 * time.Hour
)

type Config struct {
	Listen              string
	DatabaseDSN         string
	AdminUsername       string
	AdminPassword       string
	TokenSecret         string
	TokenTTL            time.Duration
	BootstrapConfigPath string
}

func LoadEnv() (Config, error) {
	cfg := Config{
		Listen:              envOrDefault("ZENHUB_SERVER_LISTEN", defaultListen),
		DatabaseDSN:         strings.TrimSpace(os.Getenv("ZENHUB_SERVER_DATABASE_DSN")),
		AdminUsername:       strings.TrimSpace(os.Getenv("ZENHUB_SERVER_ADMIN_USERNAME")),
		AdminPassword:       strings.TrimSpace(os.Getenv("ZENHUB_SERVER_ADMIN_PASSWORD")),
		TokenSecret:         strings.TrimSpace(os.Getenv("ZENHUB_SERVER_TOKEN_SECRET")),
		BootstrapConfigPath: strings.TrimSpace(os.Getenv("ZENHUB_SERVER_BOOTSTRAP_CONFIG")),
		TokenTTL:            defaultTokenTTL,
	}

	if rawTTL := strings.TrimSpace(os.Getenv("ZENHUB_SERVER_TOKEN_TTL")); rawTTL != "" {
		ttl, err := time.ParseDuration(rawTTL)
		if err != nil {
			return Config{}, fmt.Errorf("parse ZENHUB_SERVER_TOKEN_TTL: %w", err)
		}
		cfg.TokenTTL = ttl
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Listen) == "" {
		return errors.New("listen address is required")
	}
	if strings.TrimSpace(c.DatabaseDSN) == "" {
		return errors.New("ZENHUB_SERVER_DATABASE_DSN is required")
	}
	if strings.TrimSpace(c.AdminUsername) == "" {
		return errors.New("ZENHUB_SERVER_ADMIN_USERNAME is required")
	}
	if strings.TrimSpace(c.AdminPassword) == "" {
		return errors.New("ZENHUB_SERVER_ADMIN_PASSWORD is required")
	}
	if len(strings.TrimSpace(c.TokenSecret)) < 16 {
		return errors.New("ZENHUB_SERVER_TOKEN_SECRET must be at least 16 characters")
	}
	if c.TokenTTL <= 0 {
		return errors.New("token TTL must be greater than zero")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
