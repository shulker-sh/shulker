#!/bin/sh
# Installs shulker on macOS or Linux.
#
#   curl -fsSL https://shulker.sh/install.sh | sh
#
# What it does, in order:
#   1. Picks the latest release of github.com/shulker-sh/shulker, or SHULKER_VERSION.
#   2. Downloads that release's archive for your OS and CPU, and its checksums.txt.
#   3. Checks the archive's SHA256 against checksums.txt, and stops on a mismatch.
#   4. If the GitHub CLI (gh) is installed, checks the archive's build provenance: proof
#      that GitHub Actions built it in the shulker-sh organization, not someone's laptop.
#   5. Copies the shulker binary into ~/.local/bin, or SHULKER_INSTALL_DIR.
#   6. If that directory is not on your PATH, appends one line to your shell's startup
#      file (~/.zshrc, ~/.bashrc, ...) to add it.
#
# It never uses sudo. Besides the install directory and that one startup-file line, it
# only writes to a temporary directory, which it deletes on exit.
#
# Options, as a flag or an environment variable:
#   --without-attestation   SHULKER_WITHOUT_ATTESTATION=1   skip step 4
#   --require-attestation   SHULKER_REQUIRE_ATTESTATION=1   fail when step 4 can't run or fails
#   --no-modify-path        SHULKER_NO_MODIFY_PATH=1        skip step 6
#                           SHULKER_INSTALL_DIR=<dir>       install somewhere else
#                           SHULKER_VERSION=v0.0.1          install a specific release
#
# Pass flags through sh with -s --:
#
#   curl -fsSL https://shulker.sh/install.sh | sh -s -- --no-modify-path
#
# To uninstall, run `shulker self uninstall`, then delete the line marked
# "# Added by the shulker installer" from your shell's startup file.
set -eu

INSTALL_DIR="${SHULKER_INSTALL_DIR:-$HOME/.local/bin}"
WITHOUT_ATTESTATION="${SHULKER_WITHOUT_ATTESTATION:-}"
REQUIRE_ATTESTATION="${SHULKER_REQUIRE_ATTESTATION:-}"
NO_MODIFY_PATH="${SHULKER_NO_MODIFY_PATH:-}"

# Colours and progress lines are only for a terminal; NO_COLOR turns the colours off.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  green="$(printf '\033[1;32m')"
  red="$(printf '\033[1;31m')"
  grey="$(printf '\033[38;5;244m')"
  bold="$(printf '\033[1m')"
  cmd="$(printf '\033[1;36m')"
  reset="$(printf '\033[0m')"
else
  green="" red="" grey="" bold="" cmd="" reset=""
fi

if [ -t 1 ]; then
  clear_line="$(printf '\r\033[K')"
else
  clear_line=""
fi

say() { printf '%s  %s\n' "$clear_line" "$*"; }
ok() { say "${green}✔${reset} $*"; }
skip() { say "${grey}•${reset} $*"; }
fail() { say "${red}✘${reset} ${bold}$*${reset}" >&2; echo >&2; exit 1; }

# Shows what a slow step is doing; the next line printed replaces it.
working() {
  if [ -t 1 ]; then
    printf '  %s• %s…%s' "$grey" "$*" "$reset"
  fi
}

# Shows a path inside your home directory as ~/...
tilde() {
  case "$1" in
    "$HOME"/*) echo "~${1#"$HOME"}" ;;
    *) echo "$1" ;;
  esac
}

echo

for arg in "$@"; do
  case "$arg" in
    --without-attestation) WITHOUT_ATTESTATION=1 ;;
    --require-attestation) REQUIRE_ATTESTATION=1 ;;
    --no-modify-path) NO_MODIFY_PATH=1 ;;
    *) fail "Unknown flag $arg" ;;
  esac
done

case "$(uname -s)" in
  Darwin) os="darwin" os_name="macOS" ;;
  Linux) os="linux" os_name="Linux" ;;
  *) fail "Unsupported OS $(uname -s); on Windows, run irm https://shulker.sh/install.ps1 | iex" ;;
esac

case "$(uname -m)" in
  arm64 | aarch64) arch="arm64" ;;
  x86_64 | amd64) arch="amd64" ;;
  *) fail "Unsupported architecture $(uname -m)" ;;
esac

# A shell running under Rosetta reports x86_64 on Apple silicon.
if [ "$os" = "darwin" ] && [ "$arch" = "amd64" ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = "1" ]; then
  arch="arm64"
fi

# 1. Pick the release. GitHub redirects /releases/latest to /releases/tag/<version>.
version="${SHULKER_VERSION:-}"

if [ -z "$version" ]; then
  working "Finding the latest release"
  latest="$(curl -fsLI -o /dev/null -w '%{url_effective}' "https://github.com/shulker-sh/shulker/releases/latest")" || latest=""

  case "$latest" in
    */tag/*) version="${latest##*/tag/}" ;;
    *) fail "Couldn't get the latest release from https://github.com/shulker-sh/shulker/releases" ;;
  esac
fi

number="${version#v}"
version="v$number"
base="https://github.com/shulker-sh/shulker/releases/download/$version"
archive="shulker_${number}_${os}_${arch}.tar.gz"

say "${bold}Installing shulker $number for $os_name ($arch)$reset"
echo

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"

# 2. Download.
working "Downloading $archive"
curl -fsL -o "$archive" "$base/$archive" || fail "Couldn't download $base/$archive"
curl -fsL -o checksums.txt "$base/checksums.txt" || fail "Couldn't download $base/checksums.txt"
ok "Downloaded $archive"

# 3. Checksum. Each line of checksums.txt is "<sha256>  <file name>".
want="$(awk -v name="$archive" '$2 == name { print $1 }' checksums.txt)"

if [ -z "$want" ]; then
  fail "No checksum listed for $archive in checksums.txt"
fi

if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$archive" | awk '{ print $1 }')"
else
  got="$(shasum -a 256 "$archive" | awk '{ print $1 }')"
fi

if [ "$want" != "$got" ]; then
  fail "Checksum mismatch for $archive (expected $want, got $got)"
fi

ok "Checksum matches"

# 4. Build provenance. The release workflow publishes a signed attestation for each
# archive; gh checks the signature and that it names this archive and the shulker-sh owner.
verify_attestation() {
  curl -fsL -o shulker.attestation.jsonl "$base/shulker.attestation.jsonl" || return 1
  gh attestation verify "$archive" --bundle shulker.attestation.jsonl --owner shulker-sh >/dev/null 2>&1
}

if [ -n "$WITHOUT_ATTESTATION" ]; then
  skip "Build provenance not checked (--without-attestation)"
elif command -v gh >/dev/null 2>&1; then
  working "Checking build provenance"

  if verify_attestation; then
    ok "Build provenance verified"
  elif [ -n "$REQUIRE_ATTESTATION" ]; then
    fail "Build provenance couldn't be verified, and --require-attestation is set"
  else
    skip "Build provenance couldn't be verified; relying on the checksum"
  fi
elif [ -n "$REQUIRE_ATTESTATION" ]; then
  fail "--require-attestation is set, but the GitHub CLI (gh) isn't installed"
else
  skip "Build provenance not checked: install the GitHub CLI (gh) to check it"
fi

# 5. Install.
tar -xzf "$archive" shulker || fail "Couldn't unpack $archive"

if mkdir -p "$INSTALL_DIR" 2>/dev/null && install -m 0755 shulker "$INSTALL_DIR/shulker" 2>/dev/null; then
  ok "Installed to $(tilde "$INSTALL_DIR/shulker")"
else
  fail "Couldn't install to $(tilde "$INSTALL_DIR"); set SHULKER_INSTALL_DIR to a directory you can write to"
fi

# 6. PATH. The line is only added once, and is marked so you can find and remove it.
# Wrapping PATH and the directory in colons makes this match whole entries only.
case ":$PATH:" in
  *":$INSTALL_DIR:"*) on_path=1 ;;
  *) on_path="" ;;
esac

if [ -n "$on_path" ]; then
  next="Run ${cmd}shulker --help${reset} to get started."
elif [ -n "$NO_MODIFY_PATH" ]; then
  skip "$(tilde "$INSTALL_DIR") isn't on your PATH"
  next="Add $(tilde "$INSTALL_DIR") to your PATH, then run ${cmd}shulker --help${reset} to get started."
else
  line="export PATH=\"$INSTALL_DIR:\$PATH\""

  case "$(basename "${SHELL:-sh}")" in
    zsh)
      rc="${ZDOTDIR:-$HOME}/.zshrc"
      ;;
    bash)
      # macOS Terminal starts bash as a login shell, which reads .bash_profile, not .bashrc.
      if [ "$os" = "darwin" ]; then
        rc="$HOME/.bash_profile"
      else
        rc="$HOME/.bashrc"
      fi
      ;;
    fish)
      rc="$HOME/.config/fish/config.fish"
      line="fish_add_path \"$INSTALL_DIR\""
      ;;
    *)
      rc="$HOME/.profile"
      ;;
  esac

  if [ -f "$rc" ] && grep -qF "$line" "$rc"; then
    ok "$(tilde "$rc") already adds $(tilde "$INSTALL_DIR") to PATH"
  elif mkdir -p "$(dirname "$rc")" 2>/dev/null && printf '\n# Added by the shulker installer\n%s\n' "$line" 2>/dev/null >> "$rc"; then
    ok "Added $(tilde "$INSTALL_DIR") to PATH in $(tilde "$rc")"
  else
    fail "Couldn't add $(tilde "$INSTALL_DIR") to PATH in $(tilde "$rc"); rerun with --no-modify-path to skip it"
  fi

  next="Open a new terminal, then run ${cmd}shulker --help${reset} to get started."
fi

echo
say "$next"
say "Docs: https://shulker.sh/docs/getting-started"
echo
