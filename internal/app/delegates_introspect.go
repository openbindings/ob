package app

import (
	"encoding/json"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
	"github.com/openbindings/ob/internal/execref"
)

// resolveDelegateInterface fetches a delegate's OpenBindings interface from its
// location: an exec: command via its --openbindings output, an http(s) URL via
// well-known resolution, or a bare local path treated as an executable.
func resolveDelegateInterface(location string) (*openbindings.Interface, error) {
	switch {
	case delegates.IsExecURL(location):
		cmd, err := execref.RootCommand(location)
		if err != nil {
			return nil, err
		}
		iface, err := delegates.RunCLIOpenBindings(cmd, delegates.DefaultProbeTimeout)
		if err != nil {
			return nil, err
		}
		return &iface, nil
	case delegates.IsHTTPURL(location):
		data, _, err := ResolveOBI(location)
		if err != nil {
			return nil, err
		}
		var iface openbindings.Interface
		if err := json.Unmarshal(data, &iface); err != nil {
			return nil, err
		}
		return &iface, nil
	default:
		iface, err := delegates.RunCLIOpenBindings(location, delegates.DefaultProbeTimeout)
		if err != nil {
			return nil, err
		}
		return &iface, nil
	}
}

// resolvePinnedDelegateInterface resolves a registered delegate's interface
// for use and verifies it against the registration snapshot's content digest,
// so work is matched and invoked against the same document the registrar
// trusted. A digest mismatch means the delegate changed since registration;
// the safe answer is an explicit re-registration, so it is an error here,
// never a silent substitution.
func resolvePinnedDelegateInterface(rec DelegateRecord) (*openbindings.Interface, error) {
	iface, err := resolveDelegateInterface(rec.Location)
	if err != nil {
		return nil, fmt.Errorf("delegate %q: %w", rec.Location, err)
	}
	if rec.ContentHash != "" {
		hash, herr := interfaceContentHash(iface)
		if herr != nil {
			return nil, fmt.Errorf("delegate %q: %w", rec.Location, herr)
		}
		if hash != rec.ContentHash {
			return nil, fmt.Errorf(
				"delegate %q changed since it was registered (content digest mismatch); re-register it with 'ob delegate register %s' to accept the new interface",
				rec.Location, rec.Location)
		}
	}
	return iface, nil
}
