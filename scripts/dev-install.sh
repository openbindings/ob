#!/usr/bin/env bash
set -euo pipefail

# Build + link the OpenBindings CLI (`ob`) into a bin dir (default: ~/.local/bin)
# so it runs like a prod-installed CLI.
#
# Usage:
#   bash ob/scripts/dev-install.sh
#
# Options:
#   OB_BIN_DIR=...   Where to link the executables (default: ~/.local/bin)
#   OB_OUT=...       Where to place the built ob binary (default: <repo>/cli/bin/ob)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$CLI_DIR/.." && pwd)"

OUT="${OB_OUT:-$CLI_DIR/bin/ob}"

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
