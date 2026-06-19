package app

import (
	"encoding/json"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
	"github.com/openbindings/ob/internal/execref"
)

// DelegateIntrospection is what ob discovers about a delegate by resolving its
// OBI: a display name, whether it was reachable, the capabilities it provides
// (decided by compat conformance, not operation-name matching), and the formats
// it handles.
type DelegateIntrospection struct {
	Location     string
	Name         string
	Reachable    bool
	Capabilities []DelegateCapability
	Formats      []DelegateFormatInfo
}

// introspectDelegate resolves a delegate's OBI and reports what it provides.
// Capabilities come from compat against the embedded requirement interfaces;
// formats from the delegate's listFormats. An unreachable delegate yields
// Reachable=false with a location-derived name and no capabilities —
// registration still succeeds, the caller surfaces the warning.
func introspectDelegate(location string) DelegateIntrospection {
	out := DelegateIntrospection{
		Location: location,
		Name:     delegates.NameFromLocation(location),
	}

	iface, err := resolveDelegateInterface(location)
	if err != nil {
		return out // Reachable stays false
	}
	out.Reachable = true
	if iface.Name != "" {
		out.Name = iface.Name
	}
	out.Capabilities = delegateCapabilities(iface)

	// Formats come from the delegate's listFormats. Exec delegates are probed
	// directly; http delegate format discovery rides the operation-invoke path
	// (Phase 3) and is best-effort here.
	if fmts, ferr := delegates.ProbeFormats(location, delegates.DefaultProbeTimeout); ferr == nil {
		for _, f := range fmts {
			out.Formats = append(out.Formats, DelegateFormatInfo{Format: f})
		}
	}
	return out
}

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
