#!/bin/bash
# Build the Android JNI artifacts for shoruikanri (arm64-v8a):
#   build/<abi>/libgocryptfs.so      engine, c-shared, static OpenSSL, soname libgocryptfs.so
#   build/<abi>/libgocryptfs_jni.so  JNI bridge (native/gocryptfs_jni.c) for shoruikanri's package
set -e
REPO="$(cd "$(dirname "$0")" && pwd)"
ABI="${1:-arm64-v8a}"
[ "$ABI" = arm64-v8a ] || { echo "only arm64-v8a wired here" >&2; exit 1; }
GOARCH=arm64; CFN=aarch64-linux-android21-clang; OSSL=android-arm64
export GOROOT="${GOROOT:-/home/shiroikuma/goroot126/go}" GOPATH="${GOPATH:-/home/shiroikuma/go}"
export GOFLAGS=-mod=mod GOOS=android GOARCH=$GOARCH CGO_ENABLED=1
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-/home/shiroikuma/android-sdk/ndk/28.1.13356709}"
NDK_BIN="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin"
export PATH="$GOROOT/bin:$NDK_BIN:$PATH"
export CC="$NDK_BIN/$CFN" CXX="$NDK_BIN/${CFN}++"

if [ ! -f "$REPO/openssl/libcrypto.a" ]; then
  OSSL_URL=$(git -C "$REPO" config --file .gitmodules submodule.openssl.url)
  OSSL_SHA=$(git -C "$REPO" ls-tree HEAD openssl | awk '{print $3}')
  rm -rf "$REPO/openssl" "$REPO/.git/modules/openssl"
  git init -q "$REPO/openssl"; git -C "$REPO/openssl" remote add origin "$OSSL_URL"
  git -C "$REPO/openssl" fetch --depth 1 origin "$OSSL_SHA"; git -C "$REPO/openssl" checkout -q --detach FETCH_HEAD
  ( cd "$REPO/openssl" && CFLAGS=-D__ANDROID_API__=21 ANDROID_NDK_ROOT="$ANDROID_NDK_HOME" ./Configure "$OSSL" -D__ANDROID_API__=21 && make -j"$(nproc)" build_libs )
fi

PC="$REPO/.pc"; mkdir -p "$PC"
{ echo "prefix=$REPO/openssl"; echo 'libdir=${prefix}'; echo 'includedir=${prefix}/include'
  echo 'Name: libcrypto'; echo 'Description: static OpenSSL libcrypto for gcfs/android'; echo 'Version: 3.0.0'
  echo 'Libs: -L${libdir} -l:libcrypto.a'; echo 'Cflags: -I${includedir}'; } > "$PC/libcrypto.pc"
export PKG_CONFIG_LIBDIR="$PC" PKG_CONFIG_PATH=

OUT="$REPO/build/$ABI"; mkdir -p "$OUT"
echo "=== engine: libgocryptfs.so ==="
( cd "$REPO" && CGO_LDFLAGS="-Wl,-soname=libgocryptfs.so" go build -buildmode=c-shared -ldflags="-s -w" -o "$OUT/libgocryptfs.so" . )
echo "=== JNI bridge: libgocryptfs_jni.so ==="
"$CC" -shared -fPIC -Wl,-soname=libgocryptfs_jni.so -I"$OUT" "$REPO/native/gocryptfs_jni.c" -L"$OUT" -lgocryptfs -o "$OUT/libgocryptfs_jni.so"
echo "OK -> $OUT/libgocryptfs.so + libgocryptfs_jni.so"
