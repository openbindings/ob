# Invocation value migration consumer

This branch consumes the Go SDK migration candidate
`9a69a38acb5e14bf7724350ef1db6f233f13ad88`. CI checks out that exact revision
and the qualified client/corpus revisions. Published module targets remain part
of the coordinated 0.2 release process; a workspace run is not publication proof.

The application still selects its transform evaluators. The SDK owns value
snapshots, checked construction and invocation limits. The CLI owns frame
protocol grammar, transport encoding and requested JSON presentation. The
SDK-backed frame carrier now passes logical objects directly and inspects output
objects using the same strict grammar as wire decoding. Only actual serialized
source content is parsed while building its open frame.

Unsupported caller values fail at SDK admission. Such an input rejection leaves
the stream usable; a later valid value can still be written. Abandonment stops
the underlying output stream and releases its retained values. Output formatting
uses the maintained JSON codec so supported exact tokens and string values are
not silently changed by display.

Local qualification on Go 1.25.13 with version-pinned workspace replacements:
full `go test -race -short ./...`, `go vet ./...`, and final frame/application
checks passed. The SDK qualification report contains the ownership tests,
PNG transform/recovery/upload fixture, baseline GraphQL mismatch and measured
performance tradeoffs. Release activation still requires the SDK's gates,
workspace-disabled candidate verification and the corresponding CLI Linux gates.

This does not choose a new JSONata engine, change policy/delegation ownership,
introduce a storage migration, or publish a release.
