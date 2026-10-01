#!/usr/bin/env bash
# Cut a shulker release, or say what state the next one is in.
#
#   scripts/release.sh                   what is ready and what is not, and the next tags
#   scripts/release.sh rc [version]      tag the next release candidate of master on GitHub
#   scripts/release.sh final [version]   tag the release itself
#
# The version is the next patch after the newest release, or 0.0.1 before there is one; pass
# another to start a new minor or major. --yes skips the question before tagging, and --dry-run
# stops before it.
#
# A tag is made on GitHub's master, never on this checkout, so what ships is what has landed.
# The release workflow builds it; this script watches the run and prints the release's address.
#
# A candidate takes its notes from the changelog's Unreleased section. A release needs a dated
# section of its own: `final` writes it into CHANGELOG.md here when master has none, and tags
# once that has landed.
set -euo pipefail

cd "$(dirname "$0")/.."

command=status
version=""
yes=0
dry=0

for arg in "$@"; do
  case "$arg" in
    status | rc | final) command="$arg" ;;
    --yes) yes=1 ;;
    --dry-run) dry=1 ;;
    [0-9]*.[0-9]*.[0-9]*) version="$arg" ;;
    *)
      echo "usage: scripts/release.sh [status | rc | final] [version] [--yes] [--dry-run]" >&2
      exit 2
      ;;
  esac
done

problems=0

ok() { printf '  ✔ %s\n' "$1"; }
note() { printf '  ! %s\n' "$1"; }
help() { printf '    ╰─ %s\n' "$1"; }

bad() {
  printf '  ✘ %s\n' "$1"
  problems=$((problems + 1))
}

fail() {
  printf '  ✘ %s\n' "$1" >&2
  exit 1
}

api() { gh api "$@" 2>/dev/null; }

command -v gh >/dev/null 2>&1 || fail "The GitHub CLI (gh) is not installed"
gh auth status >/dev/null 2>&1 || fail "gh is not signed in; run gh auth login"

tags="$(api 'repos/shulker-sh/shulker/git/matching-refs/tags/v' --paginate -q '.[].ref' | sed 's|refs/tags/||' || true)"
newest="$(printf '%s\n' "$tags" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -t. -k1.2,1n -k2,2n -k3,3n | tail -1 || true)"

if [ -z "$version" ]; then
  if [ -z "$newest" ]; then
    version="0.0.1"
  else
    patch="${newest##*.}"
    version="${newest#v}"
    version="${version%.*}.$((patch + 1))"
  fi
fi

if printf '%s\n' "$tags" | grep -qx "v$version"; then
  fail "v$version is already tagged"
fi

candidate="$(printf '%s\n' "$tags" | sed -n "s/^v$version-rc\.\([0-9][0-9]*\)$/\1/p" | sort -n | tail -1)"
candidate="v$version-rc.$((${candidate:-0} + 1))"

head="$(api 'repos/shulker-sh/shulker/commits/master' -q '.sha')" || fail "Couldn't read master from GitHub"
subject="$(api "repos/shulker-sh/shulker/commits/$head" -q '.commit.message' | head -1)"
changelog="$(api "repos/shulker-sh/shulker/contents/CHANGELOG.md?ref=$head" -H 'Accept: application/vnd.github.raw')"

# section prints the body of one "## [name]" section of a changelog on stdin.
section() {
  awk -v name="$1" '
    index($0, "## [" name "]") == 1 { grab = 1; next }
    grab && (/^## / || /^\[[^]]+\]: /) { exit }
    grab && /[^[:space:]]/ { print }
  '
}

unreleased="$(printf '%s\n' "$changelog" | section Unreleased)"
dated="$(printf '%s\n' "$changelog" | grep -cE "^## \[$version\] - [0-9]{4}-[0-9]{2}-[0-9]{2}" || true)"

check_repo() {
  echo "GitHub"

  if [ "$(api 'repos/shulker-sh/shulker' -q '.visibility')" = "public" ]; then
    ok "The repository is public"
  else
    bad "The repository is private"
    help "Build attestations and the install scripts both need it public"
  fi

  if [ "$(api 'repos/shulker-sh/shulker/immutable-releases' -q '.enabled')" = "true" ]; then
    ok "Immutable releases are on"
  else
    bad "Immutable releases are off, so self update would refuse the release"
    help "gh api -X PUT repos/shulker-sh/shulker/immutable-releases"
  fi

  if gh secret list -R shulker-sh/shulker 2>/dev/null | grep -q '^SHULKER_CURSEFORGE_KEY'; then
    ok "The CurseForge key secret is set"
  else
    bad "The SHULKER_CURSEFORGE_KEY secret is missing"
  fi

  drafts="$(api 'repos/shulker-sh/shulker/releases' -q '[.[] | select(.draft) | .tag_name] | join(", ")')"

  if [ -n "$drafts" ]; then
    note "Draft releases a failed run left behind: $drafts"
    help "gh release delete <tag> --cleanup-tag -R shulker-sh/shulker"
  fi

  run="$(gh run list -R shulker-sh/shulker --workflow release.yml --limit 1 --json displayTitle,status,conclusion -q '.[0] | select(. != null) | "\(.displayTitle): \(.status) \(.conclusion)"' 2>/dev/null || true)"

  if [ -n "$run" ]; then
    case "$run" in
      *"completed success") ok "Last release run, $run" ;;
      *completed*) note "Last release run, $run" ;;
      *) note "A release run is in progress, $run" ;;
    esac
  fi
}

check_master() {
  echo
  echo "Master"
  ok "$(printf '%.7s' "$head") $subject"

  if [ "$(git rev-parse origin/master 2>/dev/null)" != "$head" ]; then
    note "This checkout's origin/master is behind GitHub; run but pull"
  fi

  unlanded="$(but status 2>/dev/null | grep -c '\[feature/' || true)"

  if [ "$unlanded" -gt 0 ]; then
    note "$unlanded local branches have not landed, and a tag doesn't include them"
  fi

  if [ -n "$unreleased" ]; then
    ok "Unreleased has $(printf '%s\n' "$unreleased" | grep -c '^- ') entries"
  elif [ "$dated" -eq 0 ]; then
    bad "CHANGELOG.md has nothing under Unreleased"
  fi
}

check_releases() {
  echo
  echo "Releases"
  latest="$(api 'repos/shulker-sh/shulker/releases' -q '[.[] | select(.draft | not)] | .[0] | select(. != null) | "\(.tag_name)\(if .prerelease then " (pre-release)" else "" end)"')"
  ok "Newest published: ${latest:-none yet}"
  ok "Next candidate: $candidate"

  if [ "$dated" -gt 0 ]; then
    ok "Next release: v$version, with its changelog section on master"
  else
    ok "Next release: v$version, once CHANGELOG.md has a dated section for it"
  fi
}

# prepare_changelog turns Unreleased into a dated section for the version in CHANGELOG.md here,
# and leaves an empty Unreleased above it.
prepare_changelog() {
  today="$(date +%Y-%m-%d)"
  awk -v ver="$version" -v today="$today" '
    /^## \[Unreleased\]/ {
      print
      print ""
      print "## [" ver "] - " today
      next
    }
    /^\[Unreleased\]: / {
      print "[Unreleased]: https://github.com/shulker-sh/shulker/compare/v" ver "...HEAD"
      print "[" ver "]: https://github.com/shulker-sh/shulker/releases/tag/v" ver
      next
    }
    { print }
  ' CHANGELOG.md > CHANGELOG.md.new
  mv CHANGELOG.md.new CHANGELOG.md
}

# tag makes the tag on GitHub at master, then follows the release workflow it starts.
tag() {
  if [ "$dry" -eq 1 ]; then
    echo
    note "Dry run: would tag $(printf '%.7s' "$head") as $1"
    exit 0
  fi

  if [ "$yes" -eq 0 ]; then
    echo
    printf '  Tag %.7s as %s and release it? [y/N] ' "$head" "$1"
    read -r answer

    case "$answer" in
      y | Y | yes) ;;
      *) exit 1 ;;
    esac
  fi

  gh api -X POST 'repos/shulker-sh/shulker/git/refs' -f "ref=refs/tags/$1" -f "sha=$head" >/dev/null || fail "Couldn't create $1; a deleted release's tag can't be used again"
  ok "Tagged $1"

  run=""

  for _ in 1 2 3 4 5 6 7 8 9 10; do
    run="$(gh run list -R shulker-sh/shulker --workflow release.yml --branch "$1" --limit 1 --json databaseId -q '.[0].databaseId' 2>/dev/null || true)"
    [ -n "$run" ] && break
    sleep 3
  done

  [ -n "$run" ] || fail "The release workflow didn't start; see https://github.com/shulker-sh/shulker/actions"

  if gh run watch "$run" -R shulker-sh/shulker --exit-status; then
    ok "Released https://github.com/shulker-sh/shulker/releases/tag/$1"
  else
    printf '  ✘ The release run failed: gh run view %s -R shulker-sh/shulker --log-failed\n' "$run" >&2
    help "It leaves a draft release; delete it with gh release delete $1 --cleanup-tag -R shulker-sh/shulker, fix master, and tag again"
    exit 1
  fi
}

check_repo
check_master
check_releases

if [ "$command" = "status" ]; then
  [ "$problems" -eq 0 ] || exit 1
  exit 0
fi

if [ "$problems" -gt 0 ]; then
  echo
  fail "Not released: fix the $problems marked ✘ above first"
fi

if [ "$command" = "rc" ]; then
  [ -n "$unreleased" ] || fail "A candidate takes its notes from Unreleased, which is empty on master"
  tag "$candidate"
  exit 0
fi

if [ "$dated" -eq 0 ]; then
  if grep -qE "^## \[$version\] - " CHANGELOG.md; then
    echo
    fail "CHANGELOG.md here has the v$version section, but master doesn't: land it, then run this again"
  fi

  prepare_changelog
  echo
  note "Wrote the v$version section into CHANGELOG.md from Unreleased"
  help "Commit and land it, then run scripts/release.sh final again to tag"
  exit 1
fi

tag "v$version"
