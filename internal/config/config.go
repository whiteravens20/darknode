// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

// Package config reads the server's settings from the environment.
//
// Every setting has a DARKNODE_ prefix. Secrets can be passed as files
// (DARKNODE_SECRET_FILE, DARKNODE_DATABASE_PASSWORD_FILE) so that they do not
// show up in the process environment or in `docker inspect`.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// RegistrationMode says who may create an account.
type RegistrationMode string

const (
	// RegistrationOpen lets anyone register, behind proof-of-work and rate limits.
	RegistrationOpen RegistrationMode = "open"
	// RegistrationInvite requires an invite code made with `darknode invite create`.
	RegistrationInvite RegistrationMode = "invite"
	// RegistrationClosed refuses every registration; accounts come from the operator.
	RegistrationClosed RegistrationMode = "closed"
)

// MinSecretLength is the shortest server secret accepted, in bytes.
const MinSecretLength = 32

// Config is the server's configuration.
type Config struct {
	// Listen is the address the HTTP server binds, such as ":8080".
	Listen string
	// DatabaseURL is a PostgreSQL connection string.
	DatabaseURL string
	// DatabasePassword, when set, replaces the password in DatabaseURL.
	DatabasePassword string
	// Secret keys the HMACs of recovery codes and invite codes. It never
	// leaves the server and is not stored in the database.
	Secret []byte
	// WebDir is the directory of the built web client; empty serves the API only.
	WebDir string
	// CookieSecure marks the session cookie Secure. Only a local plain-HTTP
	// development setup turns it off.
	CookieSecure bool
	// TrustedProxies are the networks whose X-Forwarded-For header is believed.
	TrustedProxies []netip.Prefix
	// Registration is who may create an account.
	Registration RegistrationMode
	// PowBits is the base difficulty of the registration proof-of-work.
	PowBits int
	// LogLevel is the lowest level that is logged.
	LogLevel slog.Level
}

// FromEnv reads the configuration through getenv, normally os.Getenv.
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		Listen:       value(getenv, "DARKNODE_LISTEN", ":8080"),
		DatabaseURL:  getenv("DARKNODE_DATABASE_URL"),
		WebDir:       getenv("DARKNODE_WEB_DIR"),
		Registration: RegistrationMode(value(getenv, "DARKNODE_REGISTRATION", string(RegistrationOpen))),
	}
	var errs []error

	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DARKNODE_DATABASE_URL is required"))
	}
	if path := getenv("DARKNODE_DATABASE_PASSWORD_FILE"); path != "" {
		password, err := readSecretFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("DARKNODE_DATABASE_PASSWORD_FILE: %w", err))
		}
		c.DatabasePassword = password
	}

	secret, err := secretFrom(getenv)
	if err != nil {
		errs = append(errs, err)
	}
	c.Secret = secret

	if c.CookieSecure, err = boolean(getenv, "DARKNODE_COOKIE_SECURE", true); err != nil {
		errs = append(errs, err)
	}

	switch c.Registration {
	case RegistrationOpen, RegistrationInvite, RegistrationClosed:
	default:
		errs = append(errs, fmt.Errorf("DARKNODE_REGISTRATION must be open, invite or closed, not %q", c.Registration))
	}

	if c.PowBits, err = integer(getenv, "DARKNODE_POW_BITS", 18, 1, 30); err != nil {
		errs = append(errs, err)
	}

	if c.TrustedProxies, err = prefixes(getenv("DARKNODE_TRUSTED_PROXIES")); err != nil {
		errs = append(errs, err)
	}

	if err := c.LogLevel.UnmarshalText([]byte(value(getenv, "DARKNODE_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("DARKNODE_LOG_LEVEL: %w", err))
	}

	return c, errors.Join(errs...)
}

// ListenPort returns the port part of Listen, for the health check.
func (c Config) ListenPort() string {
	i := strings.LastIndex(c.Listen, ":")
	if i < 0 {
		return c.Listen
	}
	return c.Listen[i+1:]
}

func secretFrom(getenv func(string) string) ([]byte, error) {
	secret := getenv("DARKNODE_SECRET")
	if path := getenv("DARKNODE_SECRET_FILE"); path != "" {
		if secret != "" {
			return nil, errors.New("set DARKNODE_SECRET or DARKNODE_SECRET_FILE, not both")
		}
		s, err := readSecretFile(path)
		if err != nil {
			return nil, fmt.Errorf("DARKNODE_SECRET_FILE: %w", err)
		}
		secret = s
	}
	if secret == "" {
		return nil, errors.New("DARKNODE_SECRET or DARKNODE_SECRET_FILE is required")
	}
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("the server secret must be at least %d characters long", MinSecretLength)
	}
	return []byte(secret), nil
}

func readSecretFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", errors.New("the file is empty")
	}
	return s, nil
}

func value(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

func boolean(getenv func(string) string, key string, fallback bool) (bool, error) {
	v := getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback, fmt.Errorf("%s must be true or false, not %q", key, v)
	}
	return b, nil
}

func integer(getenv func(string) string, key string, fallback, min, max int) (int, error) {
	v := getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return fallback, fmt.Errorf("%s must be a whole number from %d to %d, not %q", key, min, max, v)
	}
	return n, nil
}

func prefixes(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			addr, err := netip.ParseAddr(part)
			if err != nil {
				return nil, fmt.Errorf("DARKNODE_TRUSTED_PROXIES: %q is not an address or a network", part)
			}
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("DARKNODE_TRUSTED_PROXIES: %q is not an address or a network", part)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}
