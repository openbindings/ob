package delegates

import (
	"strings"

	"github.com/Masterminds/semver/v3"
)

// parseFormatToken parses a format token of the form <name>@<version-or-range>.
// If no '@' is present, it treats the whole string as the name and returns an empty version.
func parseFormatToken(s string) (name string, ver string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	at := strings.LastIndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		// No '@' or missing one side: treat as name-only.
		return strings.ToLower(strings.TrimSpace(s)), ""
	}
	name = strings.ToLower(strings.TrimSpace(s[:at]))
	ver = strings.TrimSpace(s[at+1:])
	return name, ver
}

func isExactVersionString(v string) bool {
	// Heuristic: if it contains semver constraint operators or separators, treat as a constraint.
	return !strings.ContainsAny(v, "><=^~*xX") &&
		!strings.Contains(v, "||") &&
		!strings.Contains(v, " ") &&
		!strings.Contains(v, ",")
}

func parseExactVersion(v string) (*semver.Version, bool) {
	if v == "" || !isExactVersionString(v) {
		return nil, false
	}
	ver, err := semver.NewVersion(v)
	if err != nil {
		return nil, false
	}
	return ver, true
}

func parseConstraint(v string) (*semver.Constraints, bool) {
	if v == "" || isExactVersionString(v) {
		return nil, false
	}
	c, err := semver.NewConstraint(v)
	if err != nil {
		return nil, false
	}
	return c, true
}

// supportsFormatToken returns true if delegateToken can satisfy requestedToken.
//
// Common case: delegateToken is a range (e.g., usage@^2.0.0) and requestedToken is exact
// (e.g., usage@2.0.0). This function will do semver-aware checks where possible.
func supportsFormatToken(delegateToken, requestedToken string) bool {
	pName, pVer := parseFormatToken(delegateToken)
	rName, rVer := parseFormatToken(requestedToken)
	if pName == "" || rName == "" {
		return false
	}
	if !strings.EqualFold(pName, rName) {
		return false
	}

	// Name-only delegate token supports any version/range of same name.
	if pVer == "" {
		return true
	}

	// If requested is exact, we can precisely evaluate.
	if rExact, ok := parseExactVersion(rVer); ok {
		if pExact, ok := parseExactVersion(pVer); ok {
			return pExact.Equal(rExact)
		}
		if c, ok := parseConstraint(pVer); ok {
			return c.Check(rExact)
		}
		// If delegate's version string isn't parseable, fail closed.
		return false
	}

	// Requested is a constraint/range:
	// - If delegate is exact, check if exact version satisfies requested constraint.
	if pExact, ok := parseExactVersion(pVer); ok {
		if rC, ok := parseConstraint(rVer); ok {
			return rC.Check(pExact)
		}
		return false
	}

	// Both are ranges; we conservatively approximate intersection.
	// If both are caret ranges, require same major version.
	if strings.HasPrefix(strings.TrimSpace(pVer), "^") && strings.HasPrefix(strings.TrimSpace(rVer), "^") {
		pV, errP := semver.NewVersion(strings.TrimPrefix(strings.TrimSpace(pVer), "^"))
		rV, errR := semver.NewVersion(strings.TrimPrefix(strings.TrimSpace(rVer), "^"))
		if errP == nil && errR == nil {
			return pV.Major() == rV.Major()
		}
	}
	// Otherwise, fail closed for unknown range intersection.
	return false
}
