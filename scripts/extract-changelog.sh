#!/usr/bin/env bash
# Print the release notes for a version from CHANGELOG.md, formatted for a GitHub
# release body: the version's entries only — header excluded (the version and
# date already show on the release), reference-link definitions excluded, with
# the version's reference link rendered as a single labeled link at the end.
# Usage: scripts/extract-changelog.sh 0.1.0
set -euo pipefail

version="${1:?usage: extract-changelog.sh <version>}"

# Everything under "## [version] - <date>" up to the next "## " heading or the
# link-reference block; header excluded; leading/trailing blanks trimmed. Match
# by literal prefix (index) so this behaves the same across awk implementations.
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

# The version's reference link from the bottom of the changelog. Strip the
# "[version]:" definition prefix to get the bare URL, then render it as a labeled
# inline link (a bare definition line would not show in the release body).
version_re="$(printf '%s' "$version" | sed 's/\./\\./g')"
link="$(grep -E "^\[$version_re\]: " CHANGELOG.md || true)"
url="$(printf '%s' "$link" | sed -E 's/^\[[^]]*\]:[[:space:]]*//')"

printf '%s' "$notes"
if [ -n "$url" ]; then
  printf '\n\n**Changes in v%s:** [v%s](%s)\n' "$version" "$version" "$url"
fi
