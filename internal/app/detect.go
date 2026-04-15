package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// DelegateClaim represents a delegate's claim that it can handle a source.
// Produced by running CreateInterface and inspecting the result.
type DelegateClaim struct {
	DelegateName   string // human-friendly name (e.g. "ob", "acme-openapi")
	DelegateID     string // identifier stored in x-ob.delegate: "ob" for builtin, location for external
	FormatToken    string // e.g. "openapi@3.1"
	OperationCount int
	BindingCount   int
}

// DetectSourceCandidates tries CreateInterface with every known format token
// to discover which ones can handle the given source file. Each successful
// format produces a DelegateClaim with the format token it assigned and
// the operation/binding counts from the interface it built.
func DetectSourceCandidates(location string) ([]DelegateClaim, error) {
	if !strings.HasPrefix(location, "http://") && !strings.HasPrefix(location, "https://") && !strings.HasPrefix(location, "exec:") {
		if _, err := os.Stat(location); err != nil {
			return nil, fmt.Errorf("source file not found: %s", location)
		}
	}

	var claims []DelegateClaim
	for _, fi := range DefaultCreator().Formats() {
		iface, err := CreateInterfaceFromSource(context.Background(), &openbindings.CreateInput{
			Sources: []openbindings.CreateSource{{Format: fi.Token, Location: location}},
		})
		if err != nil {
			continue
		}

		var formatToken string
		for _, src := range iface.Sources {
			if src.Format != "" {
				formatToken = src.Format
				break
			}
		}
		if formatToken == "" {
			continue
		}

		claims = append(claims, DelegateClaim{
			DelegateName:   "ob",
			DelegateID:     "ob",
			FormatToken:    formatToken,
			OperationCount: len(iface.Operations),
			BindingCount:   len(iface.Bindings),
		})
	}

	if len(claims) == 0 {
		return nil, fmt.Errorf("no delegate recognized %q; specify the format explicitly (e.g. openapi@3.1:%s)", location, location)
	}

	return claims, nil
}

// DetectSourceFormat is a convenience wrapper that returns the consensus
// format token when all delegates agree. Returns an error if delegates
// disagree or none claim the source.
func DetectSourceFormat(location string) (string, error) {
	claims, err := DetectSourceCandidates(location)
	if err != nil {
		return "", err
	}

	distinct := map[string][]string{}
	for _, c := range claims {
		distinct[c.FormatToken] = append(distinct[c.FormatToken], c.DelegateName)
	}

	if len(distinct) == 1 {
		for token := range distinct {
			return token, nil
		}
	}

	var parts []string
	for token, names := range distinct {
		parts = append(parts, fmt.Sprintf("%s (via %s)", token, strings.Join(names, ", ")))
	}
	return "", fmt.Errorf("delegates disagree on format for %q: %s; specify the format explicitly", location, strings.Join(parts, " vs "))
}
