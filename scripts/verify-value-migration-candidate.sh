#!/usr/bin/env bash
set -euo pipefail

# Same-commit SDK/core-format candidates, plus independently selected clients.
# Only temporary module overlays change; coordinated source manifests stay put.
sdk_version="${1:-}"
openapi_version="${2:-}"
asyncapi_version="${3:-}"
for version in "$sdk_version" "$openapi_version" "$asyncapi_version"; do
  if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-(0\.)?[0-9]{14}-[0-9a-f]{12}$ ]]; then
    echo 'usage: verify-value-migration-candidate.sh SDK_VERSION OPENAPI_CLIENT_VERSION ASYNCAPI_CLIENT_VERSION' >&2
    exit 2
  fi
done
repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
candidate_dir="$(mktemp -d)"
trap 'rm -rf "$candidate_dir"' EXIT
cd "$repo_dir"
export GOWORK=off
export GOFLAGS=
export OB_CORPUS_REQUIRED=1
: "${OB_SPEC_CORPUS:?set the exact spec corpus directory}"
: "${OB_INTERFACES_CORPUS:?set the exact interfaces corpus directory}"
cp go.mod "$candidate_dir/candidate.mod"
cp go.sum "$candidate_dir/candidate.sum"
cp go.mod "$candidate_dir/source.mod"
cp go.sum "$candidate_dir/source.sum"
while read -r module version; do
  go mod edit -modfile="$candidate_dir/candidate.mod" -replace="$module@$version=$module@$sdk_version"
done < <(awk '$1 ~ /^github.com\/openbindings\/openbindings-go(\/|$)/ {print $1, $2}' go.mod)
go mod edit -modfile="$candidate_dir/candidate.mod" \
  -replace="github.com/openbindings/openapi-client/go@v0.1.0=github.com/openbindings/openapi-client/go@$openapi_version" \
  -replace="github.com/openbindings/asyncapi-client/go@v0.1.0=github.com/openbindings/asyncapi-client/go@$asyncapi_version"
go mod tidy -modfile="$candidate_dir/candidate.mod"
local_replacements="$(go list -modfile="$candidate_dir/candidate.mod" -mod=readonly -m -f '{{if .Replace}}{{if not .Replace.Version}}{{.Path}}{{end}}{{end}}' all | sed '/^$/d')"
if [[ -n "$local_replacements" ]]; then
  echo "refusing local replacements: $local_replacements" >&2
  exit 1
fi
cp "$candidate_dir/candidate.mod" "$candidate_dir/readonly.mod"
cp "$candidate_dir/candidate.sum" "$candidate_dir/readonly.sum"
export GOFLAGS="-modfile=$candidate_dir/candidate.mod -mod=readonly"
go build ./...
go vet ./...
go test -race -short ./...
cmp "$candidate_dir/candidate.mod" "$candidate_dir/readonly.mod"
cmp "$candidate_dir/candidate.sum" "$candidate_dir/readonly.sum"
cmp go.mod "$candidate_dir/source.mod"
cmp go.sum "$candidate_dir/source.sum"
echo "verified CLI against SDK/core-format candidate $sdk_version with no workspace or local replacements"
