#!/usr/bin/env bash
# Write THIRD_PARTY_LICENSES: the license and notice files of every module linked
# into a release binary, for every OS the release builds, plus the Go standard
# library's. Release archives ship it beside LICENSE.
# Usage: scripts/third-party-licenses.sh [output] (default: THIRD_PARTY_LICENSES)
set -euo pipefail

out="${1:-THIRD_PARTY_LICENSES}"

# Some modules are linked on one OS only, such as mousetrap on Windows.
modules="$(
  for goos in darwin linux windows; do
    GOOS="$goos" CGO_ENABLED=0 go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' .
  done | sort -u
)"

# Homebrew's Go keeps LICENSE one folder above GOROOT.
goroot="$(go env GOROOT)"
go_license=""
for f in "$goroot/LICENSE" "$goroot/../LICENSE"; do
  if [ -f "$f" ]; then
    go_license="$f"
    break
  fi
done

if [ -z "$go_license" ]; then
  echo "error: no LICENSE in or above $goroot" >&2
  exit 1
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

section() {
  printf '\n================================================================================\n'
  printf '%s\n' "$1"
  printf -- '--------------------------------------------------------------------------------\n\n'
}

{
  printf 'shulker is built with the following third-party software.\n'

  section "Go standard library and runtime $(go env GOVERSION)"
  cat "$go_license"

  while read -r path version dir; do
    [ -n "$path" ] || continue
    files="$(find "$dir" -maxdepth 1 -type f \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'notice*' \) | sort)"

    if [ -z "$files" ]; then
      echo "error: $path $version has no license file in $dir" >&2
      exit 1
    fi

    section "$path $version"

    first=1
    while read -r f; do
      [ "$first" = 1 ] || printf '\n'
      cat "$f"
      first=0
    done <<< "$files"
  done <<< "$modules"
} > "$tmp"

mv "$tmp" "$out"
trap - EXIT
echo "wrote $out ($(printf '%s\n' "$modules" | grep -c .) modules)"
