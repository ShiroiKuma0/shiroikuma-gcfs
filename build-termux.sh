#!/bin/bash
# Cross-compile gcfs (non-FUSE gocryptfs CLI) for Termux / Android arm64.
# Produces a statically-OpenSSL-linked aarch64 binary (bionic deps only).
set -e
REPO="$(cd "$(dirname "$0")" && pwd)"

export GOROOT="${GOROOT:-$HOME/goroot126/go}"
export GOPATH="${GOPATH:-$HOME/go}"
export GOFLAGS=-mod=mod
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$HOME/android-sdk/ndk/28.1.13356709}"
NDK_BIN="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin"
export PATH="$GOROOT/bin:$NDK_BIN:$PATH"
export CC="$NDK_BIN/aarch64-linux-android21-clang"
export CXX="$NDK_BIN/aarch64-linux-android21-clang++"
export CGO_ENABLED=1 GOOS=android GOARCH=arm64

# 1. Static OpenSSL libcrypto.a for android-arm64, once.
#    Fetch the PINNED commit directly by SHA over https (shallow). This avoids
#    `git submodule update` (which trips "transport 'file' not allowed" in a
#    copied repo) and reliably gets the pin (a --depth 1 branch tip misses it).
if [ ! -f "$REPO/openssl/libcrypto.a" ]; then
  if [ ! -f "$REPO/openssl/Configure" ]; then
    OSSL_URL=$(git -C "$REPO" config --file .gitmodules submodule.openssl.url)
    OSSL_SHA=$(git -C "$REPO" ls-tree HEAD openssl | awk '{print $3}')
    rm -rf "$REPO/openssl" "$REPO/.git/modules/openssl"
    git init -q "$REPO/openssl"
    git -C "$REPO/openssl" remote add origin "$OSSL_URL"
    git -C "$REPO/openssl" fetch --depth 1 origin "$OSSL_SHA"
    git -C "$REPO/openssl" checkout -q --detach FETCH_HEAD
  fi
  export CFLAGS=-D__ANDROID_API__=21 ANDROID_NDK_ROOT="$ANDROID_NDK_HOME"
  ( cd "$REPO/openssl" && ./Configure android-arm64 -D__ANDROID_API__=21 && make -j"$(nproc)" build_libs )
fi

# 2. Pin pkg-config to the static archive (generated, absolute to this repo).
PC="$REPO/.pc"; mkdir -p "$PC"
{
  echo "prefix=$REPO/openssl"
  echo 'libdir=${prefix}'
  echo 'includedir=${prefix}/include'
  echo 'Name: libcrypto'
  echo 'Description: OpenSSL libcrypto (android-arm64 static, for gcfs)'
  echo 'Version: 3.0.0'
  echo 'Libs: -L${libdir} -l:libcrypto.a'
  echo 'Cflags: -I${includedir}'
} > "$PC/libcrypto.pc"
export PKG_CONFIG_LIBDIR="$PC" PKG_CONFIG_PATH=

# 3. Build the stripped executable.
( cd "$REPO" && go build -ldflags="-s -w" -o "$REPO/gcfs-arm64" . )
file "$REPO/gcfs-arm64"
"$NDK_BIN/llvm-readelf" -d "$REPO/gcfs-arm64" | grep NEEDED || true
echo "OK -> $REPO/gcfs-arm64"
