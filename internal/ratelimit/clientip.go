package ratelimit

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the originating client IP. When the request arrives via
// one of the configured trusted proxies, X-Forwarded-For's leftmost entry is
// honored; otherwise r.RemoteAddr wins. Untrusted requests can't spoof the
// rate-limit key by setting their own X-Forwarded-For.
func ClientIP(r *http.Request, trusted []string) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	if isTrusted(remoteHost, trusted) {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			if comma := strings.Index(xff, ","); comma >= 0 {
				xff = strings.TrimSpace(xff[:comma])
			}
			if xff != "" {
				return xff
			}
		}
	}

	if remoteHost == "" {
		return "unknown"
	}
	return remoteHost
}

func isTrusted(host string, trusted []string) bool {
	if host == "" {
		return false
	}
	for _, t := range trusted {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if host == t {
			return true
		}
		if _, ipnet, err := net.ParseCIDR(t); err == nil {
			if ip := net.ParseIP(host); ip != nil && ipnet.Contains(ip) {
				return true
			}
		}
	}
	return false
}
