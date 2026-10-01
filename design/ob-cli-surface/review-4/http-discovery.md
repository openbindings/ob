# OpenBindings HTTP Discovery (v0.1.0)

## Abstract

This companion specification defines configuration-free discovery of an OpenBindings interface document (OBI) over HTTP: a service publishes its OBI at the well-known URI path `/.well-known/openbindings`, and unconfigured clients retrieve it from there. It normatively defines the server and client obligations at that endpoint.

This document is a **companion** to the OpenBindings core specification (`openbindings.md`). It is normative for implementations that claim conformance to it, and optional for everyone else: a conformant OBI producer, consumer, or processor is not required to implement HTTP discovery, an OBI may be obtained through any mechanism without changing its meaning (core §1.4), and other discovery mechanisms (registries, configuration, service meshes) remain valid. Conformance to this specification is claimed and versioned separately from core conformance.

Retrieving an OBI through this endpoint retrieves the complete document, including any Core `dependencies` declarations. Discovery does not register, wire, select, or satisfy those dependencies; those composition behaviors remain outside both this companion and the Core specification.

## Status of this document

This is version 0.1.0 of OpenBindings HTTP Discovery. It versions independently of the core specification; the OBI documents it serves declare their own `openbindings` version, and this specification is agnostic to that value. Citations of core sections and rules in this document refer to the core specification at version 0.2.0; core rule identifiers are stable across core versions, and a core release that changes the substance of a cited section is adopted here by a revision of this specification.

## Notational conventions

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [BCP 14](https://www.rfc-editor.org/rfc/rfc2119) and [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they appear in all capitals.

## 1. Scope

A service MAY publish its OBI at the URI path `/.well-known/openbindings` on its primary origin. The `/.well-known/` namespace follows [RFC 8615](https://www.rfc-editor.org/rfc/rfc8615), which reserves this prefix for interoperable service-metadata endpoints.

This convention addresses the one-interface-per-origin case. Origins that host multiple distinct interfaces (gateways, monorepos, multi-tenant platforms) are out of scope for the well-known convention and rely on other discovery mechanisms. A `404` at this path simply means "no OBI here"; it is not an error condition.

Discovery is publication behavior, not document semantics: fetching an OBI from this path supplies no base URI to the document and changes nothing about how it resolves — the core's context-free reference model governs regardless of acquisition (core §7). Any identity or cache key a client derives from the fetched URI is its own concern.

## 2. Server contract

A conformant **discovery server** (a service serving an OBI at the well-known path):

- **DISC-S-01**: MUST answer a `GET` to `/.well-known/openbindings` with `200 OK` and a body that is a conformant OBI document (core §10.2), and MUST NOT refuse the request solely because the `Accept` header is absent or omits an OBI media type. The OBI is the path's default representation: a request without an `Accept` header receives it. Content negotiation at this path is otherwise ordinary HTTP ([RFC 9110](https://www.rfc-editor.org/rfc/rfc9110)); a publisher may serve, for example, an HTML page to a browser that prefers one, and a client sending the Accept header of DISC-C-01 always receives the OBI.
- **DISC-S-02**: SHOULD set the response `Content-Type` to `application/vnd.openbindings+json`; `application/json` MAY be used.
- **DISC-S-03**: Where the service would serve the request but publishes no OBI at the path, MUST respond `404 Not Found`. An endpoint that gates discovery behind authentication or authorization MAY instead respond `401`/`403` and need not reveal whether an OBI exists behind it; the `404` expectation applies to a request the endpoint would otherwise serve. Whether to publish an OBI to unauthenticated clients is a deployment decision; this specification does not mandate public discovery.
- **DISC-S-04**: For endpoints intended for public consumption, SHOULD set permissive CORS response headers (for example, `Access-Control-Allow-Origin: *`). Cross-origin discovery is a primary use case; omitting CORS silently breaks browser-side clients. Publishers restricting discovery to specific origins MAY set CORS headers accordingly.

## 3. Client contract

A conformant **discovery client** (a tool fetching an OBI from the well-known path):

- **DISC-C-01**: MUST accept a `200 OK` response served as `application/json` as well as one served as `application/vnd.openbindings+json`, and SHOULD send `Accept: application/vnd.openbindings+json, application/json`.
- **DISC-C-02**: SHOULD follow `3xx` redirects (`301`, `302`, `303`, `307`, `308`), subject to its own redirect policy and limits. Redirects are routine deployment artifacts (HTTP-to-HTTPS upgrades, trailing-slash normalization, CDN frontends); a client that treated them as "no OBI published" would silently disagree with one that followed them. Because an OBI's references are context-free, following a redirect does not change how the document resolves.
- **DISC-C-03**: MAY treat any response that is not a `200 OK` carrying an OBI document as "no OBI published at this path": a `404`, another `4xx`/`5xx` other than `401`/`403`, or a `200 OK` whose body the client establishes is not an OBI document — unparseable JSON, or a violation of a core document rule the client checks (an established violation, not a merely inconclusive rule; core §10.4). A `401`/`403` is gated discovery, not absence: DISC-S-03 lets publishers gate the endpoint without revealing whether an OBI exists behind it, so collapsing those statuses into "no OBI published" would discard the one signal a caller can act on (present credentials). How gated discovery is surfaced is tool-defined.
- **DISC-C-04**: MUST NOT extend DISC-C-03 to a `200 OK` carrying an OBI whose declared `openbindings` version the client refuses under the core's version-processing rules (core OBI-T-04). That outcome is a **version refusal** — the publisher's document exists but cannot be interpreted under the client's supported versions — and collapsing it into "no OBI published" would discard the one diagnostic the user can act on. How the refusal is surfaced is tool-defined.

## 4. Security considerations

Discovery inherits the core specification's considerations for processing untrusted OBI documents (core §9) and adds transport exposure of its own:

- **Plaintext discovery.** Discovery over HTTP is subject to tampering by network intermediaries; fetched content cannot be distinguished from content planted by an on-path attacker. Enforcing TLS for non-loopback origins is the ordinary mitigation.
- **Redirect handling.** Clients following DISC-C-02 apply their scheme and network-range policies per redirect hop, and distinguish the URI a document was requested at from the URI a redirect resolved to when deriving any identity or cache key (the core defines no canonical identity, and document resolution depends on neither URI).
- **Authenticated endpoints.** DISC-S-03's `401`/`403` allowance exists so publishers need not reveal the existence of gated interfaces; clients should not interpret those statuses as absence.

## 5. IANA considerations

Registration of the well-known URI suffix per [RFC 8615](https://www.rfc-editor.org/rfc/rfc8615), in the [IANA Well-Known URIs registry](https://www.iana.org/assignments/well-known-uris/well-known-uris.xhtml):

- **URI suffix:** `openbindings`
- **Change controller:** openbindings project
- **Reference:** this specification
- **Related information:** the OpenBindings core specification defines the OBI document format and registers the `application/vnd.openbindings+json` media type.

## 6. Conformance

Server and client obligations are independent conformance classes: an implementation may claim either or both, and each claim is distinct from any core OpenBindings conformance claim. The rules above carry stable identifiers (`DISC-S-##`, `DISC-C-##`) under the same stability discipline as the core's rules: identifiers are never reused or renumbered, and retired rules keep their identifiers as historical references.

## 7. References

- **[BCP 14]** RFC 2119 / BCP 14. <https://www.rfc-editor.org/rfc/rfc2119>
- **[RFC 8174]** <https://www.rfc-editor.org/rfc/rfc8174>
- **[RFC 8615]** M. Nottingham, "Well-Known Uniform Resource Identifiers (URIs)," May 2019. <https://www.rfc-editor.org/rfc/rfc8615>
- **[RFC 9110]** R. Fielding, Ed., M. Nottingham, Ed., J. Reschke, Ed., "HTTP Semantics," June 2022. <https://www.rfc-editor.org/rfc/rfc9110>
- **OpenBindings core specification** — `openbindings.md` in this repository.
