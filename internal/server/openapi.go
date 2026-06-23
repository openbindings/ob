package server

import (
	_ "embed"
	"embed"
)

//go:embed resources/*
var specResources embed.FS

// SpecResource returns the content of an embedded spec resource file.
func SpecResource(name string) ([]byte, error) {
	return specResources.ReadFile("resources/" + name)
}

//go:embed openapi.yaml
var openapiSpec []byte

// OpenAPISpec returns the embedded OpenAPI specification for the ob start API.
func OpenAPISpec() []byte {
	return openapiSpec
}

//go:embed asyncapi.yaml
var asyncapiSpec []byte

// AsyncAPISpec returns the embedded AsyncAPI specification for the ob start streaming API.
func AsyncAPISpec() []byte {
	return asyncapiSpec
}

//go:embed serve.obi.json
var serveOBI []byte

// ServeOBI returns the embedded OBI document that `ob start` publishes.
// It declares which published interfaces this binary's serve subcommand
// satisfies (software-descriptor, binding-invoker, interface-synthesizer,
// source-inspector, kv-store). The historical name "host.obi.json" predates
// the split into modular published interfaces.
func ServeOBI() []byte {
	return serveOBI
}
