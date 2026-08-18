package config

import (
	"net"
	"net/url"
	"strings"
)

// IsLoopbackOrigin reports whether an Origin header names a loopback address.
//
// Used only when AllowLoopbackOrigins is set; see the field's comment for the
// conditions. The parse is strict on purpose — "localhost.attacker.example" and
// "http://localhost@evil.example" must not pass, and a substring check on
// "localhost" would let both through.
func IsLoopbackOrigin(origin string) bool {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	// Credentials in an origin are never legitimate and are the shape of a
	// spoofing attempt.
	if u.User != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
