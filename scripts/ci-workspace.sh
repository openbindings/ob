#!/usr/bin/env bash
# Run from the ob checkout beside its exact CI dependencies.
set -euo pipefail
go work init .
awk '$1 ~ /^github.com\/openbindings\/openbindings-go/ {print $1}' go.mod | sort -u | while read -r mod; do
  rel="../openbindings-go${mod#github.com/openbindings/openbindings-go}"
  go work edit -replace "$mod=$rel"
done
go work edit -replace "github.com/openbindings/openapi-client/go=../openapi-client/go"
go work edit -replace "github.com/openbindings/asyncapi-client/go=../asyncapi-client/go"
go work edit -json
