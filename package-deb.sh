#!/bin/bash
# Build a Termux (.deb) package for the gcfs CLI (aarch64 / arm64-v8a).
# Usage: ./package-deb.sh [version]   (default 1.0.0).  Run ./build-termux.sh first.
set -e
REPO="$(cd "$(dirname "$0")" && pwd)"
VERSION="${1:-1.0.0}"
BIN="$REPO/gcfs-arm64"
[ -f "$BIN" ] || { echo "no gcfs-arm64 — run ./build-termux.sh first" >&2; exit 1; }
PREFIX="data/data/com.termux/files/usr"
STAGE="$(mktemp -d)"
mkdir -p "$STAGE/$PREFIX/bin" "$STAGE/DEBIAN"
install -m755 "$BIN" "$STAGE/$PREFIX/bin/gcfs"
SIZE=$(du -k -s "$STAGE/$PREFIX" | cut -f1)
cat > "$STAGE/DEBIAN/control" <<CTRL
Package: gcfs
Version: $VERSION
Architecture: aarch64
Maintainer: 白い熊 <claude.ai@sumou.com>
Installed-Size: $SIZE
Section: utils
Priority: optional
Homepage: https://github.com/ShiroiKuma0/shiroikuma-gcfs
Description: Non-FUSE gocryptfs CLI for Termux
 Create, browse, and incrementally sync gocryptfs volumes with no FUSE and no
 root. Subcommands: create, ls, cat, put, mkdir, stat, rm, sync, find.
 Statically links OpenSSL; depends only on bionic (no extra packages).
CTRL
OUT="$REPO/gcfs_${VERSION}_aarch64.deb"
dpkg-deb --build --root-owner-group "$STAGE" "$OUT"
rm -rf "$STAGE"
echo "built: $OUT"
