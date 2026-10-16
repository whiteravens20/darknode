// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Crockford's base32 alphabet: no I, L, O or U, so a code read aloud or
// copied by hand is hard to get wrong.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// codeLen symbols of five bits each make a 60-bit code.
const codeLen = 12

// CodeKind keeps the HMACs of different kinds of code apart, so a recovery
// code can never be used as an invite and the other way round.
type CodeKind string

// The kinds of code the server hands out.
const (
	RecoveryCode CodeKind = "recovery"
	InviteCode   CodeKind = "invite"
)

// RecoveryCodeCount is how many recovery codes an account gets.
const RecoveryCodeCount = 8

// NewCode returns a random code formatted as XXXX-XXXX-XXXX.
func NewCode() string {
	b := make([]byte, codeLen)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	var sb strings.Builder
	for i, x := range b {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(codeAlphabet[x&31])
	}
	return sb.String()
}

// ErrCodeFormat is returned for input that cannot be a code.
var ErrCodeFormat = errors.New("not a code")

// NormalizeCode turns what a person typed into the code's canonical form. It
// ignores letter case, spaces and hyphens, and reads I and L as 1 and O as 0.
func NormalizeCode(s string) (string, error) {
	var sb strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case '-', ' ':
			continue
		case 'I', 'L':
			r = '1'
		case 'O':
			r = '0'
		}
		if !strings.ContainsRune(codeAlphabet, r) {
			return "", ErrCodeFormat
		}
		sb.WriteRune(r)
	}
	if sb.Len() != codeLen {
		return "", ErrCodeFormat
	}
	return sb.String(), nil
}

// CodeMAC is what the database stores for a code: an HMAC keyed with the
// server secret, so a copy of the database alone does not reveal any code.
func CodeMAC(secret []byte, kind CodeKind, normalized string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(kind))
	m.Write([]byte{0})
	m.Write([]byte(normalized))
	return m.Sum(nil)
}

// NewSessionToken returns a random 256-bit token for the session cookie.
func NewSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// TokenHash is what the database stores for a session token.
func TokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
