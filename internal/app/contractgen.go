package app

import (
	"bytes"
	"fmt"
	"os"

	openbindings "github.com/openbindings/openbindings-go"
)

// GenerateBindingSpecSupportContract installs ob's checkBindingSpecs operation
// and verdict schema into the generated root contract without reserializing the
// hand-grouped document. The derived CLI and serve artifacts are generated from
// this updated contract in the same go-generate pass.
func GenerateBindingSpecSupportContract(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	original := append([]byte(nil), data...)

	install := [][2]string{
		{
			`An operation that corresponds to a published OpenBindings interface carries that contract's operation key as an alias (openbindings.ob.setContext carries openbindings.document-store.set), and one operation may correspond to several contracts this way, as listBindingSpecs does.`,
			`An operation that corresponds to a published OpenBindings interface carries that contract's operation key as an alias (openbindings.ob.setContext carries openbindings.document-store.set), and one operation may correspond to several contracts this way, as listBindingSpecs and checkBindingSpecs do.`,
		},
		{
			`"description": "List the binding specifications this instance can handle, by exact identifier. One operation answers the listBindingSpecs slot of every interface that has one, since the answer is the same regardless of which capability is asking."`,
			`"description": "Return an advisory glance at the native binding specifications this instance can name, for display, documentation, and pickers. Presence is a support warrant and absence carries no information; support decisions use checkBindingSpecs. One operation answers the listBindingSpecs slot of every interface that has one."`,
		},
		{
			`    "openbindings.ob.resolveInterface": {`,
			`    "openbindings.ob.checkBindingSpecs": {
      "description": "Authoritatively determine support for exact, opaque binding-specification identifier tokens. This is the only operation a consumer may use to make a support decision such as source acceptance, refusal, or delegation. The result contains one verdict per unique input token in first-occurrence order. supported is always a strict boolean; true warrants that this implementation implements the specification the exact string denotes, as published. The result is deterministic for this implementation version and the operation is side-effect free. Internal patterns never extend the warranted set or match future revisions. Future optional verdict fields may annotate a verdict but never qualify supported.",
      "aliases": [
        "openbindings.binding-invoker.checkBindingSpecs",
        "openbindings.interface-synthesizer.checkBindingSpecs"
      ],
      "idempotent": true,
      "input": {
        "type": "object",
        "properties": {
          "bindingSpecs": {
            "type": "array",
            "items": {
              "type": "string"
            },
            "description": "Exact, opaque binding-specification identifier tokens to check. The array may be empty."
          }
        },
        "required": [
          "bindingSpecs"
        ],
        "additionalProperties": false
      },
      "output": {
        "type": "array",
        "items": {
          "$ref": "#/schemas/BindingSpecVerdict"
        }
      },
      "tags": [
        "bindings"
      ]
    },
    "openbindings.ob.resolveInterface": {`,
		},
		{
			`    "ResolveInterfaceInput": {`,
			`    "BindingSpecVerdict": {
      "type": "object",
      "description": "Authoritative support verdict for one exact binding-specification identifier. Future optional fields may annotate this verdict but never qualify or soften supported.",
      "properties": {
        "bindingSpec": {
          "type": "string",
          "description": "Exact binding-specification identifier token as supplied by the caller."
        },
        "supported": {
          "type": "boolean",
          "description": "Strict support verdict. True warrants that this implementation implements the specification denoted by the exact bindingSpec string, as published; false is an unqualified refusal."
        }
      },
      "required": [
        "bindingSpec",
        "supported"
      ]
    },
    "ResolveInterfaceInput": {`,
		},
	}

	if !bytes.Contains(data, []byte(`"openbindings.ob.checkBindingSpecs"`)) {
		for _, replacement := range install {
			old, next := []byte(replacement[0]), []byte(replacement[1])
			if bytes.Count(data, old) != 1 {
				return fmt.Errorf("generate binding-spec support contract: expected one occurrence of %q", replacement[0])
			}
			data = bytes.Replace(data, old, next, 1)
		}
	}

	// Keep the public capability descriptions aligned with the actual minimal
	// operation subsets. These replacements are intentionally idempotent: the
	// root contract is an input and an output of the artifact generator, so a
	// second go-generate pass must be a no-op.
	metadata := [][2]string{
		{
			`Return the exact operation subset a delegate must satisfy to provide an ob capability (invoke → listBindingSpecs + invokeBinding, synthesize → listBindingSpecs + synthesizeInterface, inspect → listBindingSpecs + inspectSource).`,
			`Return the exact operation subset a delegate must satisfy to provide an ob capability (invoke → listBindingSpecs + checkBindingSpecs + invokeBinding, synthesize → listBindingSpecs + checkBindingSpecs + synthesizeInterface, inspect → listBindingSpecs + checkBindingSpecs + inspectSource).`,
		},
		{
			`One of ob's three binding-specification handling needs, derived from a minimal operation subset of a published interface: invoke (binding-invoker), synthesize (interface-synthesizer), inspect (source-inspector).`,
			`One of ob's three binding-specification handling needs, derived from a minimal operation subset of published interfaces: invoke (binding-invoker), synthesize (interface-synthesizer), inspect (source-inspector plus the interface-synthesizer support query).`,
		},
	}
	for _, replacement := range metadata {
		old, next := []byte(replacement[0]), []byte(replacement[1])
		switch {
		case bytes.Count(data, old) == 1:
			data = bytes.Replace(data, old, next, 1)
		case bytes.Count(data, next) == 1:
			// Already generated.
		default:
			return fmt.Errorf("generate binding-spec support contract: expected current or generated text for %q", replacement[0])
		}
	}
	if _, err := openbindings.ValidateDocument(data); err != nil {
		return fmt.Errorf("validate generated root contract: %w", err)
	}
	if bytes.Equal(data, original) {
		return nil
	}
	return AtomicWriteFile(path, data, FilePerm)
}
