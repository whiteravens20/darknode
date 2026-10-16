// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package auth

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP returns the address of the client behind r. X-Forwarded-For is
// believed only when the direct peer is a trusted proxy; the header is then
// read from the right, and the first address that is not itself a trusted
// proxy is the client. Anything a client writes into the header to its left
// is ignored.
func ClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	ip := peer.Addr().Unmap()
	if !isTrusted(ip, trusted) {
		return ip
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return ip // a malformed header: fall back to the proxy itself
		}
		hop = hop.Unmap()
		if !isTrusted(hop, trusted) {
			return hop
		}
		ip = hop
	}
	return ip
}

func isTrusted(ip netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// LimitKey groups addresses for rate limits: an IPv4 address on its own, an
// IPv6 address by its /64, the block one household or host usually holds.
func LimitKey(ip netip.Addr) string {
	if !ip.IsValid() {
		return "unknown"
	}
	ip = ip.Unmap()
	if ip.Is4() {
		return ip.String()
	}
	p, err := ip.Prefix(64)
	if err != nil {
		return ip.String()
	}
	return p.String()
}
