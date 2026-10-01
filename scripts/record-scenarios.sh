#!/usr/bin/env bash
# Records the CLI's scenarios from the live services, or runs them live.
#
#   scripts/record-scenarios.sh [name…]            fill each recording with what it lacks
#   scripts/record-scenarios.sh --replace name…    record the named scenarios wholesale
#   scripts/record-scenarios.sh --live [name…]     run against the live services, fixtures untouched
#
# No names means every scenario under internal/cli/testdata/scenarios.
set -euo pipefail

cd "$(dirname "$0")/.."

mode=(-record)
replace=false
names=()
for arg in "$@"; do
  case "$arg" in
    --live) mode=(-live) ;;
    --replace) replace=true ;;
    -*)
      echo "error: unknown flag $arg" >&2
      exit 2
      ;;
    *) names+=("$arg") ;;
  esac
done

if $replace; then
  if [ "${mode[0]}" = "-live" ]; then
    echo "error: --replace and --live can't go together: --live leaves the recordings alone" >&2
    exit 2
  fi
  if [ ${#names[@]} -eq 0 ]; then
    echo "error: --replace records scenarios wholesale, so name them" >&2
    exit 2
  fi
  mode+=(-replace)
fi

key="${SHULKER_CURSEFORGE_KEY:-}"

if [ -z "$key" ]; then
  key="$(security find-generic-password -s shulker-curseforge -w 2>/dev/null || true)"
fi

if [ -z "$key" ]; then
  echo "error: no CurseForge key; set SHULKER_CURSEFORGE_KEY or store one with:" >&2
  echo "  security add-generic-password -a \"\$USER\" -s shulker-curseforge -w" >&2
  exit 1
fi

run='^TestScenarios$'
if [ ${#names[@]} -gt 0 ]; then
  run="^TestScenarios\$/^($(IFS='|'; echo "${names[*]}"))\$"
fi

SHULKER_CURSEFORGE_KEY="$key" go test -count=1 -timeout 60m -v ./internal/cli -run "$run" "${mode[@]}"
