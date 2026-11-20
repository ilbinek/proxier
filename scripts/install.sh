#!/bin/sh
# proxier installer for Linux (systemd).
#
# Downloads a release from GitHub, verifies its SHA-256 checksum and runs
# `proxier install`, which creates the "proxier" user, /var/lib/proxier,
# /etc/proxier/proxier.env and the systemd service. Running it again upgrades
# proxier in place and keeps your config and data.
#
#   curl -fsSL https://github.com/ilbinek/proxier/releases/latest/download/install.sh | sudo sh
#   curl -fsSL https://github.com/ilbinek/proxier/releases/latest/download/install.sh | sudo sh -s -- --listen :8443 --tls self-signed
#
# Arguments after `--` are passed to `proxier install` (see `proxier install -h`).
# Environment:
#   PROXIER_VERSION=v1.2.3   install a specific release instead of the latest
#   PROXIER_REPO=owner/repo  download from another GitHub repository
set -eu

REPO="${PROXIER_REPO:-ilbinek/proxier}"
VERSION="${PROXIER_VERSION:-latest}"

say() { printf '\033[1m==>\033[0m %s\n' "$*"; }
die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = "Linux" ] || die "this installer supports Linux only (download a release archive for other systems)"
[ "$(id -u)" -eq 0 ] || die "run as root, e.g.: curl -fsSL <url> | sudo sh"
command -v systemctl >/dev/null 2>&1 || die "systemd is required (systemctl not found); see the README for running with Docker instead"

case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  armv7* | armv8l) ARCH=armv7 ;;
  *) die "unsupported CPU architecture: $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 3 -o "$2" "$1"; }
  fetch_stdout() { curl -fsSL --retry 3 "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
  fetch_stdout() { wget -q -O - "$1"; }
else
  die "curl or wget is required"
fi

if [ "$VERSION" = "latest" ]; then
  say "Looking up the latest release of $REPO"
  VERSION="$(fetch_stdout "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  [ -n "$VERSION" ] || die "could not determine the latest release (is there a published release in $REPO?)"
fi

ARCHIVE="proxier_${VERSION#v}_linux_${ARCH}.tar.gz"
BASE="https://github.com/$REPO/releases/download/$VERSION"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

say "Downloading proxier $VERSION (linux/$ARCH)"
fetch "$BASE/$ARCHIVE" "$TMP/$ARCHIVE" || die "download failed: $BASE/$ARCHIVE"
fetch "$BASE/checksums.txt" "$TMP/checksums.txt" || die "download failed: $BASE/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  say "Verifying checksum"
  (cd "$TMP" && grep "[ *]$ARCHIVE$" checksums.txt | sha256sum -c - >/dev/null) || die "checksum mismatch for $ARCHIVE"
else
  say "sha256sum not found, skipping checksum verification"
fi

tar -xzf "$TMP/$ARCHIVE" -C "$TMP" proxier
chmod +x "$TMP/proxier"

if [ -x /usr/local/bin/proxier ]; then
  say "Upgrading $(/usr/local/bin/proxier version 2>/dev/null || echo proxier) to $VERSION"
else
  say "Installing proxier $VERSION"
fi
"$TMP/proxier" install "$@"
