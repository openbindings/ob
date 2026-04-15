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

// OpenAPISpec returns the embedded OpenAPI specification for the ob serve API.
func OpenAPISpec() []byte {
	return openapiSpec
}

//go:embed asyncapi.yaml
var asyncapiSpec []byte

// AsyncAPISpec returns the embedded AsyncAPI specification for the ob serve streaming API.
func AsyncAPISpec() []byte {
	return asyncapiSpec
}

//go:embed host.obi.json
var hostOBI []byte

// HostOBI returns the embedded host OpenBindings interface.
func HostOBI() []byte {
	return hostOBI
}
