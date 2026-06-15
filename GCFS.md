# gcfs — non-FUSE gocryptfs CLI (fork additions)

Fork of hardcore-sushi/libgocryptfs (kept as the `upstream` remote) that adds a
command-line front-end, `gcfs`, for gocryptfs volumes with no FUSE and no root —
e.g. on an unrooted Android phone via Termux.

## Added on top of upstream
- `cli.go`  — CLI: create, ls, cat, put, mkdir, stat, rm, sync (+ Go-native listDir)
- `sync.go` — incremental size+mtime mirror (encrypt/decrypt), -base, -delete, -n, -checksum
- `common_ops.go` — `gcf_set_mtime` (preserve plaintext mtime on the ciphertext file)
- `main.go` — entry point delegated to the CLI
- `build-termux.sh` — cross-compile a static-OpenSSL aarch64 Termux binary (NDK)

## Build (Termux / arm64)
    ANDROID_NDK_HOME=/path/to/ndk ./build-termux.sh    # inits the openssl submodule + builds
    # -> ./gcfs-arm64  (push to the phone, e.g. adb push ./gcfs-arm64 /sdcard/tmp/gcfs)

## Build (host, for testing)
    CGO_ENABLED=1 go build -o gcfs .                   # links system libcrypto

## Usage
    GOCRYPTFS_PASSWORD=... gcfs create /path/vol
    gcfs [-passfile F] sync [-n] [-delete] [-base /p] <SRC> <DST>
    # one side is a volume (has gocryptfs.conf): plain->vol encrypts, vol->plain decrypts.

See docs/ for the host and on-device test scripts.

## Android JNI artifacts (for shoruikanri)

`./build-jni.sh arm64-v8a` builds, into `build/arm64-v8a/`:
- `libgocryptfs.so` — engine as a c-shared lib (static OpenSSL, soname `libgocryptfs.so`).
- `libgocryptfs_jni.so` — JNI bridge (`native/gocryptfs_jni.c`, lifted from DroidFS and
  re-pointed at `me.zhanghai.android.files.provider.gocryptfs.client`), DT_NEEDED `libgocryptfs.so`.

Drop both into shoruikanri `app/src/main/jniLibs/arm64-v8a/`; the Kotlin side loads `gocryptfs`
then `gocryptfs_jni`. mtime is whole seconds (no ms conversion).
