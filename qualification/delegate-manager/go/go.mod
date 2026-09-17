// Independent Delegate Manager consumer harness. It imports only the public
// OpenBindings Go SDK modules: no ob package, no manager helper. It discovers
// the candidate's OBIs and invokes the shared operations through the SDK's
// ordinary operation invocation over the published Usage and OpenAPI bindings.
//
// Module pins mirror the candidate's own go.mod exactly (recorded in the
// qualification ledger); the replace lines exist because the SDK's format
// modules require an untagged root version.
module github.com/openbindings/ob/qualification/delegate-manager/go

go 1.25.12

require (
	github.com/coder/websocket v1.8.15
	github.com/openbindings/openbindings-go v0.2.0
	github.com/openbindings/openbindings-go/formats/asyncapi v0.1.1-0.20260917182618-48ba4edd3567
	github.com/openbindings/openbindings-go/formats/openapi v0.1.1-0.20260911030355-4b176954a677
	github.com/openbindings/openbindings-go/formats/usage v0.1.1-0.20260917203834-e894be7a6463
)

require (
	github.com/calico32/kdl-go v0.15.0 // indirect
	github.com/cockroachdb/apd/v3 v3.2.1 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/dlclark/regexp2/v2 v2.7.1 // indirect
	github.com/getkin/kin-openapi v0.149.0 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/golang/snappy v0.0.1 // indirect
	github.com/linkedin/goavro/v2 v2.15.0 // indirect
	github.com/oasdiff/yaml v0.1.1 // indirect
	github.com/oasdiff/yaml3 v0.0.14 // indirect
	github.com/openbindings/asyncapi-client/go v0.1.0 // indirect
	github.com/openbindings/jsonata/go v0.0.0-20260910174534-e2a5e518e6b5 // indirect
	github.com/openbindings/openapi-client/go v0.1.0 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/openbindings/asyncapi-client/go v0.1.0 => github.com/openbindings/asyncapi-client/go v0.0.0-20260917182212-7326ce4e18c1
	github.com/openbindings/openapi-client/go v0.1.0 => github.com/openbindings/openapi-client/go v0.0.0-20260911032503-9f8020e2d9a7
	github.com/openbindings/openbindings-go v0.2.0 => github.com/openbindings/openbindings-go v0.1.1-0.20260911030355-4b176954a677
)
