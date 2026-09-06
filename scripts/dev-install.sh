#!/usr/bin/env bash
set -euo pipefail

# Build + link the OpenBindings CLI (`ob`) into a bin dir (default: ~/.local/bin)
# so it runs like a prod-installed CLI.
#
# Usage:
#   bash ob/scripts/dev-install.sh
#
# Options:
#   OB_BIN_DIR=...        Where to link the executables (default: ~/.local/bin)
#   OB_OUT=...            Where to place the built ob binary (default: <repo>/cli/bin/ob)
#   OB_SKIP_WORKBENCH=1   Skip rebuilding the embedded workbench assets
#   OB_LOCAL_WORKSPACE=0  Do not normalize sibling client modules in go.work

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$CLI_DIR/.." && pwd)"

OUT="${OB_OUT:-$CLI_DIR/bin/ob}"

prepare_local_client_workspace() {
  local workspace_file="$CLI_DIR/go.work"
  local client_rel client_dir module_path

  if [ "${OB_LOCAL_WORKSPACE:-1}" = "0" ] || [ ! -f "$workspace_file" ]; then
    return 0
  fi

  # A local client selected only through a workspace replace contributes its
  # source files but not its own go.mod requirements to workspace version
  # selection. That can compile new client code against an older transitive
  # dependency selected by an SDK adapter. Make each available sibling client
  # a workspace module instead, so source and dependency graph move together.
  for client_rel in \
    "../openapi-client/go" \
    "../asyncapi-client/go"
  do
    client_dir="$CLI_DIR/$client_rel"
    if [ ! -f "$client_dir/go.mod" ]; then
      continue
    fi

    module_path="$(awk '$1 == "module" { print $2; exit }' "$client_dir/go.mod")"
    if [ "$module_path" = "" ]; then
      echo "error: no module directive in $client_dir/go.mod" >&2
      return 1
    fi

    echo "Workspace: using local $module_path"
    (
      cd "$CLI_DIR"
      GOWORK="$workspace_file" go work use "$client_rel"
      GOWORK="$workspace_file" go work edit -dropreplace="$module_path"
    )
  done
}

pick_link_target() {
  # If user provided an override, use it.
  if [ "${OB_BIN_DIR:-}" != "" ]; then
    echo "$OB_BIN_DIR/ob"
    return 0
  fi

  # If ob already exists on PATH, update that location
  if command -v ob >/dev/null 2>&1; then
    existing="$(command -v ob)"
    # Resolve symlinks to find the real location
    if [ -L "$existing" ]; then
      # It's a symlink - we can replace it
      echo "$existing"
    else
      # It's a real binary - replace it with our symlink
      echo "$existing"
    fi
    return 0
  fi

  # Default: ~/.local/bin
  echo "$HOME/.local/bin/ob"
}

LINK="$(pick_link_target)"
BIN_DIR="$(dirname "$LINK")"

echo "Repo:      $REPO_ROOT"
echo "CLI:       $CLI_DIR"
echo "Build out: $OUT"
echo "Link:      $LINK"
echo ""

mkdir -p "$(dirname "$OUT")"
mkdir -p "$BIN_DIR"

prepare_local_client_workspace

# Rebuild the embedded workbench assets so `go build` doesn't bake in a stale
# dist. The elements workspace owns the source; its build writes into
# ob/internal/server/workbench/dist (see that directory's README).
ELEMENTS_DIR="$REPO_ROOT/elements"
if [ "${OB_SKIP_WORKBENCH:-}" = "1" ]; then
  echo "Skipping workbench assets (OB_SKIP_WORKBENCH=1)."
elif [ ! -d "$ELEMENTS_DIR" ]; then
  echo "warning: elements workspace not found at $ELEMENTS_DIR;"
  echo "         using existing embedded workbench dist (may be stale)."
elif ! command -v pnpm >/dev/null 2>&1; then
  echo "warning: pnpm not found; using existing embedded workbench dist (may be stale)."
  echo "         Install pnpm or run: pnpm --dir \"$ELEMENTS_DIR\" build:workbench"
else
  echo "Building workbench assets..."
  pnpm --dir "$ELEMENTS_DIR" build:workbench
fi

echo "Building..."
(cd "$CLI_DIR" && go build -o "$OUT" ./cmd/ob)

echo "Linking..."
# Remove existing file/symlink first to ensure clean replacement
rm -f "$LINK"
ln -sf "$OUT" "$LINK"

echo "Verifying..."
if command -v ob >/dev/null 2>&1; then
  echo "  ok: ob is on PATH ($(command -v ob))"
else
  echo "  warning: ob not found on PATH in this shell."
  echo "  - linked to: $LINK"
  echo "  - you may need: hash -r"
  echo "  - or add to ~/.zshrc: export PATH=\"$BIN_DIR:\$PATH\""
fi

echo ""
echo "Done."
echo ""
