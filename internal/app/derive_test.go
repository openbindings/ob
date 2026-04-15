package app

import (
	"testing"

	"github.com/openbindings/openbindings-go"
)

func TestDetectCrossSourceDrift_NoDrift(t *testing.T) {
	// Two sources produce the same operation with identical schemas.
	perSource := []perSourceDerivation{
		{
			key:    "sourceA",
			format: "usage@2.0.0",
			result: DeriveResult{
				Operations: map[string]openbindings.Operation{
					"greet": {
						Input: map[string]any{"type": "string"},
					},
				},
			},
		},
		{
			key:    "sourceB",
			format: "openapi@3.1",
			result: DeriveResult{
				Operations: map[string]openbindings.Operation{
					"greet": {
						Input: map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	drift := detectCrossSourceDrift(perSource)
	if len(drift) != 0 {
		t.Errorf("expected no drift, got %d entries", len(drift))
	}
}

func TestDetectCrossSourceDrift_SchemaDiffers(t *testing.T) {
	// Two sources produce the same operation but with different input schemas.
	perSource := []perSourceDerivation{
		{
			key:    "restApi",
			format: "openapi@3.1",
			result: DeriveResult{
				Operations: map[string]openbindings.Operation{
					"createUser": {
						Input: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":  map[string]any{"type": "string"},
								"email": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
		{
			key:    "mcpTools",
			format: "mcp@1.0",
			result: DeriveResult{
				Operations: map[string]openbindings.Operation{
					"createUser": {
						Input: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{"type": "string"},
								// missing email — drift!
							},
						},
					},
				},
			},
		},
	}

	drift := detectCrossSourceDrift(perSource)
	if len(drift) != 1 {
		t.Fatalf("expected 1 drift entry, got %d", len(drift))
	}

	d := drift[0]
	if d.Operation != "createUser" {
		t.Errorf("expected operation 'createUser', got %q", d.Operation)
	}
	if len(d.Sources) != 2 {
		t.Errorf("expected 2 sources in drift, got %d", len(d.Sources))
	}
	if len(d.Details) == 0 {
		t.Error("expected drift details")
	}
}

func TestDetectCrossSourceDrift_SingleSource(t *testing.T) {
	// Single source — no drift possible.
	perSource := []perSourceDerivation{
		{
			key:    "sourceA",
			format: "usage@2.0.0",
			result: DeriveResult{
				Operations: map[string]openbindings.Operation{
					"greet": {},
				},
			},
		},
	}

	drift := detectCrossSourceDrift(perSource)
	if len(drift) != 0 {
		t.Errorf("expected no drift with single source, got %d", len(drift))
	}
}

func TestDetectCrossSourceDrift_DisjointOperations(t *testing.T) {
	// Two sources with no overlapping operations — no drift.
	perSource := []perSourceDerivation{
		{
			key:    "sourceA",
			format: "usage@2.0.0",
			result: DeriveResult{
				Operations: map[string]openbindings.Operation{
					"greet": {},
				},
			},
		},
		{
			key:    "sourceB",
			format: "openapi@3.1",
			result: DeriveResult{
				Operations: map[string]openbindings.Operation{
					"farewell": {},
				},
			},
		},
	}

	drift := detectCrossSourceDrift(perSource)
	if len(drift) != 0 {
		t.Errorf("expected no drift with disjoint operations, got %d", len(drift))
	}
}

func TestDeriveFromAllSources_OnlySourceFilter(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"src1": {Format: "usage@2.0.0", Location: "cli.kdl"},
			"src2": {Format: "openapi@3.1", Location: "api.json"},
		},
	}

	// Requesting a nonexistent source should error.
	_, err := deriveFromAllSources(iface, "", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}

func TestDeriveFromAllSources_EmptySources(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"empty": {Format: "usage@2.0.0"}, // no location or content
		},
	}

	result, err := deriveFromAllSources(iface, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning for empty source")
	}
	if len(result.PerSource) != 0 {
		t.Errorf("expected 0 per-source results, got %d", len(result.PerSource))
	}
}

func TestMakeRelativeToDir(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		dir     string
		wantRel bool
	}{
		{
			name:    "relative to parent",
			path:    "/project/sub/cli.kdl",
			dir:     "/project",
			wantRel: true,
		},
		{
			name:    "same directory",
			path:    "/project/cli.kdl",
			dir:     "/project",
			wantRel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeRelativeToDir(tt.path, tt.dir)
			if tt.wantRel && result == tt.path {
				t.Errorf("expected relative path, got %q", result)
			}
		})
	}
}
