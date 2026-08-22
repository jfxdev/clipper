package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ipv6ClientPrefix is the prefix length used to group IPv6 clients for rate
// limiting and quotas. A single subscriber is routinely handed a whole /64
// (2^64 addresses), so bucketing per exact address makes every per-client
// limit trivially bypassable — and lets one client allocate unbounded
// server-side state.
const ipv6ClientPrefix = 64

// clientAddr resolves the address a request should be attributed to.
//
// X-Forwarded-For is "client, hop1, hop2, ..." — each proxy appends what it
// saw, so entries on the left are the ones furthest from us and entirely
// attacker-controlled. It is therefore only consulted when the direct peer
// is a proxy we actually trust, and then walked from the right, skipping
// hops that are themselves trusted, until the first address we did not
// vouch for: that is the real client.
func clientAddr(r *http.Request, trustProxy bool, trusted []netip.Prefix) netip.Addr {
	peer := parseAddr(remoteHost(r.RemoteAddr))

	if !peerIsTrusted(peer, trustProxy, trusted) {
		return peer
	}
	fwd := r.Header.Get("X-Forwarded-For")
	if fwd == "" {
		return peer
	}

	parts := strings.Split(fwd, ",")
	var first netip.Addr
	for i := len(parts) - 1; i >= 0; i-- {
		addr := parseAddr(strings.TrimSpace(parts[i]))
		if !addr.IsValid() {
			// A malformed hop means we can no longer reason about what is
			// to its left, so stop and use the last good address.
			break
		}
		first = addr
		if !inPrefixes(addr, trusted) {
			return addr
		}
	}
	if first.IsValid() {
		// Every hop was a trusted proxy; the leftmost is the best guess.
		return first
	}
	return peer
}

func peerIsTrusted(peer netip.Addr, trustProxy bool, trusted []netip.Prefix) bool {
	if !peer.IsValid() {
		return false
	}
	if inPrefixes(peer, trusted) {
		return true
	}
	// TRUST_PROXY is the blunt legacy switch: trust whoever connected to
	// us. Only safe when nothing but the reverse proxy can reach the port.
	return trustProxy
}

func inPrefixes(addr netip.Addr, prefixes []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func parseAddr(s string) netip.Addr {
	addr, err := netip.ParseAddr(strings.Trim(s, "[]"))
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// clientKey collapses an address into the key used for per-client limits:
// the exact address for IPv4, the routed /64 for IPv6.
func clientKey(addr netip.Addr) string {
	if !addr.IsValid() {
		return "unknown"
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	prefix, err := addr.Prefix(ipv6ClientPrefix)
	if err != nil {
		return addr.String()
	}
	return prefix.String()
}

// clientIP is the string form used by the limiters, derived from the
// request's trusted address.
func clientIP(r *http.Request, trustProxy bool, trusted []netip.Prefix) string {
	return clientKey(clientAddr(r, trustProxy, trusted))
}
