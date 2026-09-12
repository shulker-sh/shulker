#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

key="${SHULKER_CURSEFORGE_KEY:-}"
if [ -z "$key" ]; then
  key="$(security find-generic-password -s shulker-curseforge -w 2>/dev/null || true)"
fi
if [ -z "$key" ]; then
  echo "error: no CurseForge key; set SHULKER_CURSEFORGE_KEY or store one with:" >&2
  echo "  security add-generic-password -a \"\$USER\" -s shulker-curseforge -w" >&2
  exit 1
fi

mkdir -p dist
CGO_ENABLED=0 go build \
  -ldflags "-s -w -X shulker.sh/shulker/internal/provider/curseforge.embeddedKey=$key" \
  -o dist/shulker ./cmd/shulker
echo "built dist/shulker with an embedded CurseForge key"

if [ "${1:-}" != "--check" ]; then
  exit 0
fi

bin="$PWD/dist/shulker"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
# Unset the env var and point at an empty config so only the embedded key can be used.
unset SHULKER_CURSEFORGE_KEY
export SHULKER_CONFIG="$tmp/config.json"
"$bin" init --yes >/dev/null
"$bin" add --provider curseforge jei
echo "embedded key works"
