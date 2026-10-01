package cmd

import (
	"net/netip"
	"strings"
)

// Keep URI components in their authored spelling. net/url decodes userinfo
// and folds scheme case; neither change belongs in OBI-D-13's comparison.
type nxURI struct {
	scheme, authority, path, query, fragment string
	hasAuthority, hasQuery, hasFragment      bool
}

// nxParseURI checks the URI-reference grammar of RFC 3986 Appendix A.
// It does not check a scheme's own requirements or normalize components.
func nxParseURI(raw string) (nxURI, bool) {
	var u nxURI
	raw, u.fragment, u.hasFragment = strings.Cut(raw, "#")
	raw, u.query, u.hasQuery = strings.Cut(raw, "?")
	if nxURIScheme.MatchString(raw) {
		u.scheme, raw, _ = strings.Cut(raw, ":")
	}
	if strings.HasPrefix(raw, "//") {
		u.hasAuthority = true
		u.authority, u.path, _ = strings.Cut(raw[2:], "/")
		if strings.Contains(raw[2:], "/") {
			u.path = "/" + u.path
		}
		if !nxURIAuthority(u.authority) {
			return u, false
		}
	} else {
		u.path = raw
		if u.scheme == "" && strings.Contains(strings.SplitN(raw, "/", 2)[0], ":") {
			return u, false // path-noscheme's first segment excludes colon.
		}
	}
	return u, nxURIChars(u.path, ":@/", true) &&
		nxURIChars(u.query, ":@/?", true) && nxURIChars(u.fragment, ":@/?", true)
}

func nxURIHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func nxURIChars(s, extra string, percent bool) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' && percent {
			if i+2 >= len(s) || !nxURIHex(s[i+1]) || !nxURIHex(s[i+2]) {
				return false
			}
			i += 2
			continue
		}
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			strings.ContainsRune("-._~!$&'()*+,;="+extra, rune(c)) {
			continue
		}
		return false
	}
	return true
}

func nxURIAuthority(authority string) bool {
	host := authority
	if user, rest, found := strings.Cut(authority, "@"); found {
		if !nxURIChars(user, ":", true) {
			return false
		}
		host = rest
	}
	var port string
	if strings.HasPrefix(host, "[") {
		literal, rest, closed := strings.Cut(host[1:], "]")
		if !closed || rest != "" && !strings.HasPrefix(rest, ":") {
			return false
		}
		port = strings.TrimPrefix(rest, ":")
		if len(literal) > 1 && (literal[0] == 'v' || literal[0] == 'V') {
			version, address, dot := strings.Cut(literal[1:], ".")
			if !dot || version == "" || address == "" || !nxURIChars(address, ":", false) {
				return false
			}
			for i := range version {
				if !nxURIHex(version[i]) {
					return false
				}
			}
		} else if ip, err := netip.ParseAddr(literal); err != nil || !ip.Is6() || ip.Zone() != "" {
			return false
		}
	} else {
		host, port, _ = strings.Cut(host, ":")
		if !nxURIChars(host, "", true) {
			return false
		}
	}
	for i := range port {
		if port[i] < '0' || port[i] > '9' {
			return false
		}
	}
	return true
}

func (u nxURI) String() string {
	var s strings.Builder
	if u.scheme != "" {
		s.WriteString(u.scheme + ":")
	}
	if u.hasAuthority {
		s.WriteString("//" + u.authority)
	}
	s.WriteString(u.path)
	if u.hasQuery {
		s.WriteString("?" + u.query)
	}
	if u.hasFragment {
		s.WriteString("#" + u.fragment)
	}
	return s.String()
}

// nxResolveID follows RFC 3986 section 5.2's strict resolution algorithm.
// It returns false for an invalid reference or a relative reference without
// an absolute, valid base, as required for inclusion in OBI-D-13.
func nxResolveID(base, ref string) (string, bool) {
	r, ok := nxParseURI(ref)
	if !ok {
		return "", false
	}
	if r.scheme != "" {
		r.path = nxRemoveDotSegments(r.path)
		return r.String(), true
	}
	b, ok := nxParseURI(base)
	if !ok || b.scheme == "" {
		return "", false
	}
	r.scheme = b.scheme
	if r.hasAuthority {
		r.path = nxRemoveDotSegments(r.path)
	} else {
		r.authority, r.hasAuthority = b.authority, b.hasAuthority
		if r.path == "" {
			r.path = b.path
			if !r.hasQuery {
				r.query, r.hasQuery = b.query, b.hasQuery
			}
		} else {
			if !strings.HasPrefix(r.path, "/") {
				if b.hasAuthority && b.path == "" {
					r.path = "/" + r.path
				} else {
					r.path = b.path[:strings.LastIndex(b.path, "/")+1] + r.path
				}
			}
			r.path = nxRemoveDotSegments(r.path)
		}
	}
	return r.String(), true
}

// This deliberately preserves double slashes, trailing slashes, percent
// encodings, and component case. path.Clean would change too much.
func nxRemoveDotSegments(input string) string {
	output := ""
	for input != "" {
		switch {
		case strings.HasPrefix(input, "../"):
			input = input[3:]
		case strings.HasPrefix(input, "./"):
			input = input[2:]
		case strings.HasPrefix(input, "/./") || input == "/.":
			input = "/" + strings.TrimPrefix(input[2:], "/")
		case strings.HasPrefix(input, "/../") || input == "/..":
			input = "/" + strings.TrimPrefix(input[3:], "/")
			if last := strings.LastIndex(output, "/"); last >= 0 {
				output = output[:last]
			} else {
				output = ""
			}
		case input == "." || input == "..":
			input = ""
		default:
			start := 0
			if input[0] == '/' {
				start = 1
			}
			end := len(input)
			if slash := strings.IndexByte(input[start:], '/'); slash >= 0 {
				end = start + slash
			}
			output, input = output+input[:end], input[end:]
		}
	}
	return output
}
