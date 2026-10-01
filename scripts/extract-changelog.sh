#!/usr/bin/env bash
# Print the release notes for a version from CHANGELOG.md, formatted for a GitHub
# release body: the version's entries only — header excluded (the version and
# date already show on the release), reference-link definitions excluded, with
# the version's reference link rendered as a single labeled link at the end.
# Usage: scripts/extract-changelog.sh 0.1.0
set -euo pipefail

version="${1:?usage: extract-changelog.sh <version>}"

# index() matches the heading as a literal prefix; a regex would need the version's
# dots escaped, and awk implementations disagree on how.
notes="$(awk -v ver="$version" '
  index($0, "## [" ver "]") == 1 { grab = 1; next }
  grab && (/^## / || /^\[[^]]+\]: /) { exit }
  grab { lines[n++] = $0 }
  END {
    s = 0;     while (s < n  && lines[s] ~ /^[[:space:]]*$/) s++
    e = n - 1; while (e >= s && lines[e] ~ /^[[:space:]]*$/) e--
    for (i = s; i <= e; i++) print lines[i]
  }
' CHANGELOG.md)"

# A bare reference definition does not render in a release body, so the URL is
# re-emitted as an inline link.
version_re="$(printf '%s' "$version" | sed 's/\./\\./g')"
link="$(grep -E "^\[$version_re\]: " CHANGELOG.md || true)"
url="$(printf '%s' "$link" | sed -E 's/^\[[^]]*\]:[[:space:]]*//')"

printf '%s' "$notes"

# Unreleased has no tag to link to: it is what a pre-release ships.
if [ -n "$url" ] && [ "$version" != "Unreleased" ]; then
  printf '\n\n**Changes in v%s:** [v%s](%s)\n' "$version" "$version" "$url"
fi
