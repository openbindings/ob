# Embedded workbench assets

These files are generated from the sibling
[`openbindings/elements`](https://github.com/openbindings/elements) workspace
and embedded into the `ob` binary. Do not hand-edit `dist`.

From a development checkout where `elements` and `ob` are siblings:

```sh
pnpm --dir ../elements build:workbench
```

The elements workspace owns the source, browser tests, and package boundaries.
The `ob` repository owns the embedding, public route, security headers, and
live server integration tests. The generated asset names are stable and all
responses use `Cache-Control: no-store`, so a newly started binary cannot serve
stale browser code.
