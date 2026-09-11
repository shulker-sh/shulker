#!/bin/sh
# shulker installer for macOS and Linux. Downloads a release from GitHub, verifies its
# SHA256 checksum and (when gh is installed) its build provenance, installs the binary,
# and adds the install directory to PATH.
#
#   curl -fsSL https://shulker.sh/install.sh | sh
#   curl -fsSL https://shulker.sh/install.sh | sh -s -- --no-modify-path
#
# Environment / flags:
#   SHULKER_INSTALL_DIR=~/.local/bin   install location (default)
#   SHULKER_VERSION=v0.0.1             install a specific tag (default: latest)
#   --without-attestation              skip the build provenance check
#   --require-attestation              fail unless build provenance is verified
#   --no-modify-path                   don't add the install directory to a shell rc file
set -eu

OWNER="shulker-sh"
REPO="shulker"
INSTALL_DIR="${SHULKER_INSTALL_DIR:-$HOME/.local/bin}"
WITHOUT_ATTESTATION="${SHULKER_WITHOUT_ATTESTATION:-}"
REQUIRE_ATTESTATION="${SHULKER_REQUIRE_ATTESTATION:-}"
NO_MODIFY_PATH="${SHULKER_NO_MODIFY_PATH:-}"

err() { echo "shulker install: $*" >&2; exit 1; }
note() { echo "==> $*"; }

for arg in "$@"; do
  case "$arg" in
    --without-attestation) WITHOUT_ATTESTATION=1 ;;
    --require-attestation) REQUIRE_ATTESTATION=1 ;;
    --no-modify-path) NO_MODIFY_PATH=1 ;;
    *) echo "shulker install: unknown flag $arg" >&2; exit 2 ;;
  esac
done

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) err "unsupported OS $(uname -s); on Windows run: irm https://shulker.sh/install.ps1 | iex" ;;
esac
case "$(uname -m)" in
  arm64 | aarch64) arch="arm64" ;;
  x86_64 | amd64) arch="amd64" ;;
  *) err "unsupported architecture $(uname -m)" ;;
esac
# A shell running under Rosetta reports x86_64 on Apple silicon.
if [ "$os" = "darwin" ] && [ "$arch" = "amd64" ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = "1" ]; then
  arch="arm64"
fi

version="${SHULKER_VERSION:-}"
if [ -z "$version" ]; then
  note "resolving latest release"
  latest="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$OWNER/$REPO/releases/latest")" \
    || err "could not reach GitHub to resolve the latest release"
  version="${latest##*/tag/}"
  [ "$version" != "$latest" ] || err "no release found at https://github.com/$OWNER/$REPO/releases"
fi
base="https://github.com/$OWNER/$REPO/releases/download/$version"
archive="shulker_${version#v}_${os}_${arch}.tar.gz"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"

note "downloading $archive"
curl -fsSL -o "$archive" "$base/$archive" || err "download failed: $base/$archive"
curl -fsSL -o checksums.txt "$base/checksums.txt" || err "could not download $base/checksums.txt"

note "verifying SHA256 checksum"
want="$(awk -v f="$archive" '$2 == f { print $1 }' checksums.txt)"
[ -n "$want" ] || err "no checksum listed for $archive"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$archive" | awk '{ print $1 }')"
else
  got="$(shasum -a 256 "$archive" | awk '{ print $1 }')"
fi
[ "$want" = "$got" ] || err "checksum mismatch for $archive (want $want, got $got)"

verify_attestation() {
  curl -fsSL -o shulker.attestation.jsonl "$base/shulker.attestation.jsonl" 2>/dev/null || return 1
  gh attestation verify "$archive" --bundle shulker.attestation.jsonl --owner "$OWNER" >/dev/null 2>&1
}

if [ -n "$WITHOUT_ATTESTATION" ]; then
  note "skipping build provenance check (--without-attestation)"
elif command -v gh >/dev/null 2>&1; then
  note "verifying build provenance with gh"
  if verify_attestation; then
    note "build provenance verified"
  elif [ -n "$REQUIRE_ATTESTATION" ]; then
    err "build provenance could not be verified and --require-attestation is set"
  else
    note "build provenance could not be verified, continuing on the checksum"
  fi
elif [ -n "$REQUIRE_ATTESTATION" ]; then
  err "--require-attestation is set but gh is not installed"
else
  note "gh not found, skipping build provenance check"
fi

note "installing to $INSTALL_DIR"
tar -xzf "$archive" shulker
mkdir -p "$INSTALL_DIR"
install -m 0755 shulker "$INSTALL_DIR/shulker"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    if [ -n "$NO_MODIFY_PATH" ]; then
      note "$INSTALL_DIR is not on your PATH"
    else
      case "$(basename "${SHELL:-sh}")" in
        zsh) rc="${ZDOTDIR:-$HOME}/.zshrc"; line="export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
        bash)
          if [ "$os" = "darwin" ]; then rc="$HOME/.bash_profile"; else rc="$HOME/.bashrc"; fi
          line="export PATH=\"$INSTALL_DIR:\$PATH\""
          ;;
        fish) rc="$HOME/.config/fish/config.fish"; line="fish_add_path \"$INSTALL_DIR\"" ;;
        *) rc="$HOME/.profile"; line="export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
      esac
      if [ -f "$rc" ] && grep -qF "$line" "$rc"; then
        note "$rc already adds $INSTALL_DIR to PATH; open a new terminal to use shulker"
      else
        mkdir -p "$(dirname "$rc")"
        printf '\n# Added by the shulker installer\n%s\n' "$line" >> "$rc"
        note "added $INSTALL_DIR to PATH in $rc; open a new terminal to use shulker"
      fi
    fi
    ;;
esac

echo "shulker ${version#v} installed to $INSTALL_DIR/shulker"
echo
echo "Get started: https://shulker.sh/docs/getting-started"
