// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package auth

import (
	"bytes"
	"regexp"
	"testing"
)

func TestNewCodeFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		c := NewCode()
		if !re.MatchString(c) {
			t.Fatalf("bad code %q", c)
		}
		if seen[c] {
			t.Fatalf("code %q repeated within 1000", c)
		}
		seen[c] = true
	}
}

func TestNormalizeCode(t *testing.T) {
	code := NewCode()
	canonical, err := NormalizeCode(code)
	if err != nil {
		t.Fatal(err)
	}
	for _, typed := range []string{
		code,
		" " + code + " ",
		toLower(code),
		removeHyphens(code),
	} {
		got, err := NormalizeCode(typed)
		if err != nil || got != canonical {
			t.Errorf("NormalizeCode(%q) = %q, %v; want %q", typed, got, err, canonical)
		}
	}
	if got, _ := NormalizeCode("OIL0-1111-2222"); got != "011011112222" {
		t.Errorf("look-alike letters not mapped: %q", got)
	}
	for _, bad := range []string{"", "ABCD-EFGH", "ABCD-EFGH-JKMN-P", "ABCD-EFGH-JKMU", "ABCD-EFGH-JK!N"} {
		if _, err := NormalizeCode(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestCodeMACSeparatesKindsAndSecrets(t *testing.T) {
	a := CodeMAC([]byte("secret-one"), RecoveryCode, "ABCDEFGHJKMN")
	if !bytes.Equal(a, CodeMAC([]byte("secret-one"), RecoveryCode, "ABCDEFGHJKMN")) {
		t.Fatal("the HMAC is not deterministic")
	}
	if bytes.Equal(a, CodeMAC([]byte("secret-one"), InviteCode, "ABCDEFGHJKMN")) {
		t.Fatal("a recovery code and an invite with the same text share an HMAC")
	}
	if bytes.Equal(a, CodeMAC([]byte("secret-two"), RecoveryCode, "ABCDEFGHJKMN")) {
		t.Fatal("the HMAC does not depend on the secret")
	}
}

func TestSessionTokens(t *testing.T) {
	a, b := NewSessionToken(), NewSessionToken()
	if a == b || len(a) < 43 {
		t.Fatalf("weak tokens: %q %q", a, b)
	}
	if bytes.Equal(TokenHash(a), TokenHash(b)) || len(TokenHash(a)) != 32 {
		t.Fatal("token hashes collide or have the wrong size")
	}
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func removeHyphens(s string) string { return string(bytes.ReplaceAll([]byte(s), []byte("-"), nil)) }
