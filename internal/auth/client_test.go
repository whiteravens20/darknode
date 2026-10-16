// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package auth

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16"), netip.MustParsePrefix("10.0.0.1/32")}
	cases := []struct {
		name, remote string
		xff          []string
		want         string
	}{
		{"direct client, header ignored", "198.51.100.7:4000", []string{"1.2.3.4"}, "198.51.100.7"},
		{"behind one proxy", "172.18.0.2:4000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"spoofed left part ignored", "172.18.0.2:4000", []string{"6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		{"two proxies", "10.0.0.1:4000", []string{"198.51.100.7, 172.18.0.9"}, "198.51.100.7"},
		{"header split over lines", "172.18.0.2:4000", []string{"6.6.6.6", "198.51.100.7"}, "198.51.100.7"},
		{"malformed header", "172.18.0.2:4000", []string{"not-an-ip"}, "172.18.0.2"},
		{"no header behind proxy", "172.18.0.2:4000", nil, "172.18.0.2"},
		{"mapped IPv4", "[::ffff:198.51.100.7]:4000", nil, "198.51.100.7"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		for _, v := range tc.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := ClientIP(r, trusted).String(); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestLimitKey(t *testing.T) {
	cases := map[string]string{
		"198.51.100.7":         "198.51.100.7",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::1": "2001:db8:1:2::/64",
		"2001:db8:1:3:3:4:5:6": "2001:db8:1:3::/64",
		"::ffff:198.51.100.7":  "198.51.100.7",
	}
	for in, want := range cases {
		if got := LimitKey(netip.MustParseAddr(in)); got != want {
			t.Errorf("LimitKey(%s) = %s, want %s", in, got, want)
		}
	}
	if LimitKey(netip.Addr{}) != "unknown" {
		t.Error("an invalid address needs a key too")
	}
}
