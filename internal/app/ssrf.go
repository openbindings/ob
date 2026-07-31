package app

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ValidateOutboundURL enforces SSRF protection on EVERY outbound fetch ob
// makes on a caller's behalf — OBI resolution via /resolve AND source-content
// reads (status, source pull, synthesize/addSource, merge), which resolve
// refs embedded in a possibly-untrusted document. It allows loopback/localhost
// (ob is a local dev tool; reaching locally-running services is a primary use
// case) but blocks other private, link-local, and cloud-metadata ranges to
// prevent LAN scanning and metadata theft.
//
// This lives in package app so the one guard serves both outbound paths; the
// command layer's /resolve handler delegates here rather than keeping its own
// copy, so the two can never drift.
func ValidateOutboundURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return &url.Error{Op: "fetch", URL: rawURL, Err: errNonHTTPScheme}
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return &url.Error{Op: "fetch", URL: rawURL, Err: errEmptyHost}
	}

	ip := net.ParseIP(hostname)
	if ip != nil {
		if !ip.IsLoopback() && isPrivateIP(ip) {
			return &url.Error{Op: "fetch", URL: rawURL, Err: errPrivateIP}
		}
	} else if !strings.EqualFold(hostname, "localhost") {
		addrs, err := net.LookupHost(hostname)
		if err == nil {
			for _, a := range addrs {
				if resolved := net.ParseIP(a); resolved != nil && !resolved.IsLoopback() && isPrivateIP(resolved) {
					return &url.Error{Op: "fetch", URL: rawURL, Err: errPrivateIP}
				}
			}
		}
	}

	return nil
}

// The ranges core §9.1 (Recommended mitigations) enumerates: link-local,
// loopback, private, and carrier-grade NAT. Go's IPNet.Contains normalizes
// IPv4-mapped IPv6 forms before comparison, which §9.1 also calls for.
var privateRanges = []*net.IPNet{
	parseCIDR("0.0.0.0/8"),
	parseCIDR("10.0.0.0/8"),
	parseCIDR("172.16.0.0/12"),
	parseCIDR("192.168.0.0/16"),
	parseCIDR("127.0.0.0/8"),
	parseCIDR("169.254.0.0/16"),
	parseCIDR("100.64.0.0/10"), // carrier-grade NAT (core §9.1)
	parseCIDR("::1/128"),
	parseCIDR("fc00::/7"),
	parseCIDR("fe80::/10"),
}

// GuardedHTTPClient returns a client that applies ValidateOutboundURL to
// EVERY hop, not just the first. Core §9.1 requires the check "after DNS
// resolution, per redirect hop": without this, a public URL that 302s to
// http://169.254.169.254/... defeats a first-URL-only guard, because Go
// follows redirects by default.
func GuardedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if err := ValidateOutboundURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect hop refused: %w", err)
			}
			return nil
		},
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, r := range privateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

func parseCIDR(s string) *net.IPNet {
	_, n, _ := net.ParseCIDR(s)
	return n
}

type ssrfError string

func (e ssrfError) Error() string { return string(e) }

const (
	errNonHTTPScheme ssrfError = "only http and https schemes are allowed"
	errEmptyHost     ssrfError = "empty hostname"
	errPrivateIP     ssrfError = "private/internal IP addresses are not allowed"
)
