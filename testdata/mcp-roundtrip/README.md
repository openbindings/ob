# MCP round-trip corpus

This directory defines the external corpus policy for the differential profile
in `internal/cmd/mcp_test.go`. External servers are not fetched by ordinary
unit tests.

## Tiers

1. **Hermetic differential fixture** — runs on every change. It covers tools,
   static resources, resource templates, prompts, complete descriptors,
   structured and multimodal content, result metadata, protocol-native tool
   errors, progress, and cancellation.
2. **Pinned upstream fixtures** — scheduled or explicitly invoked. Start with:
   - the official MCP Everything server at the `2025.11.25` release;
   - official TypeScript SDK Streamable HTTP examples at the SDK version used
     by `@openbindings/mcp`;
   - an official Python SDK FastMCP example pinned to an immutable revision;
   - the official MCP conformance runner pinned to release `v0.1.16`.
3. **Real-server canaries** — scheduled, non-blocking until minimized into a
   hermetic regression: GitHub's official MCP server, Cloudflare's MCP
   servers, and Sentry's MCP server.

Every adopted upstream fixture records an immutable commit or release, setup
command, expected revision, represented feature inventory, named exclusions,
and required credentials. Floating default branches are never CI inputs.

## Comparison profile

For each source:

1. run the upstream MCP conformance suite against the original server;
2. exhaust and save all four list families;
3. synthesize with both Go and TypeScript, with embedded listing content;
4. compare canonical OBIs and synthesis coverage;
5. bridge the Go-produced OBI through `ob mcp`;
6. run the same conformance suite against the bridge;
7. compare inventories, complete descriptors, representative successful and
   failing calls, progress/result ordering, and cancellation; and
8. classify every difference as a specification defect, synthesis defect,
   binding-implementation defect, bridge defect, declared exclusion, or
   upstream-fixture issue.

An external failure becomes release-blocking only after it is reproduced
hermetically or shown to violate a normative rule without relying on mutable
server state.
