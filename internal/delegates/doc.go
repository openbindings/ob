// Package delegates provides external delegate management: discovery, resolution,
// probing, and IPC for binding format delegates.
//
// Built-in format support is handled by the format repos (openapi-go, grpc-go, etc.)
// composed via openbindings.OperationExecutor. This package manages the external
// delegate lifecycle: finding delegates, probing their capabilities,
// and resolving which delegate handles a given format.
package delegates
