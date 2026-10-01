# Document Store

A generic store of named JSON documents: `get`, `set`, and `delete` whole JSON objects by opaque key.

"Document" here is used in the **document-database** sense — any JSON object, keyed and stored whole. It is *not* specifically an OpenBindings document (though an OBI, being a JSON object, is a perfectly valid thing to store). The key is an opaque string and the document is any JSON object; the interface prescribes neither a key-derivation scheme nor a schema for the object beyond "it is a JSON object." That is the whole point — it is the minimum surface that lets independent stores of keyed JSON be interchangeable.

## Operations

- `openbindings.document-store.get` — retrieve the document for a key; returns `null` if there is no entry.
- `openbindings.document-store.set` — store a document for a key, replacing any existing document in full.
- `openbindings.document-store.delete` — remove the entry for a key; idempotent (deleting a missing key succeeds).

## Positioning: an optional at-rest companion

This interface is **not required by any implementation**. The [`binding-invoker`](../binding-invoker/) contract carries context by value and may challenge for missing requirements; it prescribes no storage architecture. A document store is one place context-shaped state can **durably live at rest** when a substitutable storage seam is useful. An implementation may instead use a private store, a credential broker, caller-only context, or no persistence.

Like every interface in this catalog it is **non-normative**: the core spec requires it of no one, and conformance is claimed independently of every other layer (an implementation may carry `get`/`set` alone and be fully usable for those; checking a whole contract for full correspondence is a separate, opt-in assertion).

## Using it as a context store

A **context store** is this interface used with caller-chosen scope keys, holding context objects carried by `binding-invoker`. The store itself never learns that: it sees an opaque key and an opaque JSON object. Everything domain-specific about that pairing —

- what a context object contains (the `bearerToken` / `apiKey` / `basic` / `headers` / … well-known field conventions), and
- how a storage key is derived, normalized, or scoped

— belongs to the caller or runtime, **not** to this store. See binding-invoker's *Context* section for the shared field taxonomy. A runtime resolving `CONTEXT_REQUIRED` may persist durable results here, but `durable: true` permits persistence rather than requiring it.

Confidentiality is likewise a **deployment-boundary** concern, not a property of this contract: whether documents are encrypted, access-controlled, or redacted on read depends on where the store runs and who can reach it. By contract, `get` returns what `set` stored. Because `set`'s `value` is always a JSON object, no entry ever holds `null`, so `get`'s `null` uniformly means "no entry" and never an entry holding null; to remove an entry, use `delete`.

## Generally reusable

Nothing here is OpenBindings-specific. Any ecosystem participant can use a document store for its own keyed JSON state — configuration blobs, cached artifacts, session records, or OBIs themselves — without touching the context loop at all. The context-store pairing above is one application of a deliberately generic contract, not its definition.

## Not prescribed

Listing keys, inspection, rotation, auditing, TTL/expiry, and scoping are all outside this contract. An implementation that wants a richer management surface exposes additional operations; consumers that need only whole-document lookup and replacement can depend on this minimal surface.
