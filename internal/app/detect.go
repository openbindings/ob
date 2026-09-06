package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openbindings/openbindings-go/synthesize"
)

// probeTimeout bounds each per-format synthesis probe during detection.
// Detection is a liveness question: a format whose synthesizer cannot
// answer quickly (e.g. a gRPC reflection dial against something that is
// not a gRPC server) is a non-claim, not a hang.
const probeTimeout = 5 * time.Second

// DelegateClaim represents a delegate's claim that it can handle a source.
// Produced by running SynthesizeInterface and inspecting the result.
type DelegateClaim struct {
	DelegateName   string // human-friendly name (e.g. "ob", "acme-openapi")
	DelegateID     string // identifier stored in x-ob.delegate: "ob" for builtin, location for external
	BindingSpec    string // exact identifier, e.g. "openbindings.openapi-3.1@1"
	OperationCount int
	BindingCount   int
}

// DetectSourceCandidates tries SynthesizeInterface with every known format token
// to discover which ones can handle the given source file. Each successful
// format produces a DelegateClaim with the format token it assigned and
// the operation/binding counts from the interface it built.
func DetectSourceCandidates(location string) ([]DelegateClaim, error) {
	var claims []DelegateClaim
	var retrievalErr error
	if !strings.HasPrefix(location, "http://") && !strings.HasPrefix(location, "https://") && !strings.HasPrefix(location, "exec:") {
		if _, err := os.Stat(location); err != nil {
			return nil, fmt.Errorf("source file not found: %s", location)
		}
	}
	// Byte acquisition and optional provider recognition are separate from
	// validation. Never turn a failed retrieval or recognized invalid document
	// into a claim that the format is unknown. Non-byte services keep probing.
	if strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") || IsEmbeddableLocalFile(location, "") {
		data, err := ReadSourceContent(location, "")
		if err != nil {
			// HTTP locations may be non-artifact services (for example MCP).
			// Keep their existing synthesis probes before surfacing a GET failure.
			retrievalErr = fmt.Errorf("retrieve source: %w", err)
		} else if recognized, handled, err := recognizedSource(data, location); handled {
			if err != nil {
				return nil, err
			}
			claims = append(claims, recognized...)
		}
	}

	// Each OpenAPI sibling claims only its own artifact edition. Auto-detection
	// keeps the first successful exact identifier in one family while explicit
	// identifiers remain exact everywhere else.
	claimedFamilies := map[string]bool{}
	for _, claim := range claims {
		claimedFamilies[SpecFamily(claim.BindingSpec)] = true
	}
	for _, fi := range DefaultSynthesizer().BindingSpecs() {
		family := SpecFamily(fi.BindingSpec)
		if claimedFamilies[family] {
			continue
		}
		if claim, ok := probeFormatClaim(synthesize.SynthesizeSource{BindingSpec: fi.BindingSpec, Location: location}); ok {
			claims = append(claims, claim)
			claimedFamilies[family] = true
		}
	}

	if len(claims) == 0 {
		if retrievalErr != nil {
			return nil, retrievalErr
		}
		return nil, fmt.Errorf("could not recognize or validate the source format; select an exact binding specification to inspect its document errors")
	}

	return claims, nil
}

// detectCandidatesFromBytes probes raw artifact bytes with every known format
// token — the detection lane for a stdin-supplied artifact, which has no
// location to re-read. Each probe carries the bytes as embedded content
// (converted per the candidate format's own content convention), so probing
// never dials and never touches the filesystem; formats whose content carrier
// rejects the bytes are non-claims.
func detectCandidatesFromBytes(data []byte) ([]DelegateClaim, error) {
	var claims []DelegateClaim
	if recognized, handled, err := recognizedSource(data, ""); handled {
		if err != nil {
			return nil, err
		}
		claims = append(claims, recognized...)
	}
	// See DetectSourceCandidates: one automatic claim per binding family;
	// callers can still request any exact sibling identifier explicitly.
	claimedFamilies := map[string]bool{}
	for _, claim := range claims {
		claimedFamilies[SpecFamily(claim.BindingSpec)] = true
	}
	for _, fi := range DefaultSynthesizer().BindingSpecs() {
		family := SpecFamily(fi.BindingSpec)
		if claimedFamilies[family] {
			continue
		}
		content, err := ParseContentForEmbed(data, fi.BindingSpec)
		if err != nil {
			continue
		}
		if claim, ok := probeFormatClaim(synthesize.SynthesizeSource{BindingSpec: fi.BindingSpec, Content: content}); ok {
			claims = append(claims, claim)
			claimedFamilies[family] = true
		}
	}

	if len(claims) == 0 {
		return nil, fmt.Errorf("could not detect the format of the stdin artifact; specify it explicitly (e.g. openbindings.openapi-3.1@1:-)")
	}

	return claims, nil
}

func recognizedSource(data []byte, location string) ([]DelegateClaim, bool, error) {
	for _, recognize := range defaultCLIRuntime().Recognizers {
		spec, claimed, err := recognize(data)
		if !claimed {
			continue
		}
		if err != nil {
			return nil, true, fmt.Errorf("recognized source edition is unsupported or invalid: %w", err)
		}
		content, err := ParseContentForEmbed(data, spec)
		if err != nil {
			return nil, true, fmt.Errorf("recognized %s document: %w", spec, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		iface, err := SynthesizeInterfaceFromSource(ctx, &synthesize.SynthesizeInput{Sources: []synthesize.SynthesizeSource{{BindingSpec: spec, Location: location, Content: content}}})
		cancel()
		if err != nil {
			return nil, true, fmt.Errorf("recognized %s document could not be synthesized: %w", spec, err)
		}
		return []DelegateClaim{{DelegateName: "ob", DelegateID: "ob", BindingSpec: spec, OperationCount: len(iface.Operations), BindingCount: len(iface.Bindings)}}, true, nil
	}
	return nil, false, nil
}

// probeFormatClaim runs one bounded synthesis probe and returns the claim
// when the candidate format's synthesizer accepts the source.
func probeFormatClaim(source synthesize.SynthesizeSource) (DelegateClaim, bool) {
	probeCtx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	iface, err := SynthesizeInterfaceFromSource(probeCtx, &synthesize.SynthesizeInput{
		Sources: []synthesize.SynthesizeSource{source},
	})
	if err != nil {
		return DelegateClaim{}, false
	}

	var formatToken string
	for _, src := range iface.Sources {
		if src.BindingSpec != "" {
			formatToken = src.BindingSpec
			break
		}
	}
	if formatToken == "" {
		return DelegateClaim{}, false
	}

	return DelegateClaim{
		DelegateName:   "ob",
		DelegateID:     "ob",
		BindingSpec:    formatToken,
		OperationCount: len(iface.Operations),
		BindingCount:   len(iface.Bindings),
	}, true
}

// DetectSourceFormat is a convenience wrapper that returns the consensus
// format token when all delegates agree. Returns an error if delegates
// disagree or none claim the source.
func DetectSourceFormat(location string) (string, error) {
	claims, err := DetectSourceCandidates(location)
	if err != nil {
		return "", err
	}
	return consensusFormat(claims, fmt.Sprintf("%q", location))
}

// DetectSourceFormatFromBytes is DetectSourceFormat for a stdin-supplied
// artifact: the consensus format token probed from the raw bytes.
func DetectSourceFormatFromBytes(data []byte) (string, error) {
	claims, err := detectCandidatesFromBytes(data)
	if err != nil {
		return "", err
	}
	return consensusFormat(claims, "the stdin artifact")
}

// consensusFormat reduces detection claims to a single format token, or an
// error naming the disagreeing candidates. The subject is already formatted
// for the error message (a quoted location, or "the stdin artifact").
func consensusFormat(claims []DelegateClaim, subject string) (string, error) {
	distinct := map[string][]string{}
	for _, c := range claims {
		distinct[c.BindingSpec] = append(distinct[c.BindingSpec], c.DelegateName)
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
	return "", fmt.Errorf("delegates disagree on format for %s: %s; specify the format explicitly", subject, strings.Join(parts, " vs "))
}
