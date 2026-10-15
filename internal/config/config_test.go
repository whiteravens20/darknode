// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const goodSecret = "0123456789abcdef0123456789abcdef"

func TestDefaults(t *testing.T) {
	c, err := FromEnv(env(map[string]string{
		"DARKNODE_DATABASE_URL": "postgres://localhost/darknode",
		"DARKNODE_SECRET":       goodSecret,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" || !c.CookieSecure || c.Registration != RegistrationOpen || c.PowBits != 18 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.ListenPort() != "8080" {
		t.Fatalf("ListenPort = %q", c.ListenPort())
	}
}

func TestRequiredSettings(t *testing.T) {
	_, err := FromEnv(env(nil))
	if err == nil {
		t.Fatal("an empty environment was accepted")
	}
	for _, want := range []string{"DARKNODE_DATABASE_URL", "DARKNODE_SECRET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %s: %v", want, err)
		}
	}
}

func TestShortSecretRefused(t *testing.T) {
	_, err := FromEnv(env(map[string]string{
		"DARKNODE_DATABASE_URL": "postgres://localhost/darknode",
		"DARKNODE_SECRET":       "too-short",
	}))
	if err == nil {
		t.Fatal("a short secret was accepted")
	}
}

func TestSecretsFromFiles(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret")
	passwordPath := filepath.Join(dir, "password")
	if err := os.WriteFile(secretPath, []byte(goodSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwordPath, []byte("hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := FromEnv(env(map[string]string{
		"DARKNODE_DATABASE_URL":           "postgres://localhost/darknode",
		"DARKNODE_SECRET_FILE":            secretPath,
		"DARKNODE_DATABASE_PASSWORD_FILE": passwordPath,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if string(c.Secret) != goodSecret || c.DatabasePassword != "hunter2" {
		t.Fatalf("secrets not read from files: %q %q", c.Secret, c.DatabasePassword)
	}
}

func TestBothSecretSourcesRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte(goodSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := FromEnv(env(map[string]string{
		"DARKNODE_DATABASE_URL": "postgres://localhost/darknode",
		"DARKNODE_SECRET":       goodSecret,
		"DARKNODE_SECRET_FILE":  path,
	}))
	if err == nil {
		t.Fatal("two secret sources were accepted")
	}
}

func TestInvalidValuesRefused(t *testing.T) {
	cases := map[string]string{
		"DARKNODE_REGISTRATION":    "sometimes",
		"DARKNODE_POW_BITS":        "40",
		"DARKNODE_COOKIE_SECURE":   "maybe",
		"DARKNODE_TRUSTED_PROXIES": "not-a-network",
		"DARKNODE_LOG_LEVEL":       "loud",
	}
	for key, bad := range cases {
		_, err := FromEnv(env(map[string]string{
			"DARKNODE_DATABASE_URL": "postgres://localhost/darknode",
			"DARKNODE_SECRET":       goodSecret,
			key:                     bad,
		}))
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%q was accepted or the error does not name it: %v", key, bad, err)
		}
	}
}

func TestTrustedProxies(t *testing.T) {
	c, err := FromEnv(env(map[string]string{
		"DARKNODE_DATABASE_URL":    "postgres://localhost/darknode",
		"DARKNODE_SECRET":          goodSecret,
		"DARKNODE_TRUSTED_PROXIES": "172.18.0.0/16, 10.0.0.5 ,fd00::/8",
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, p := range c.TrustedProxies {
		got = append(got, p.String())
	}
	want := "172.18.0.0/16 10.0.0.5/32 fd00::/8"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}
