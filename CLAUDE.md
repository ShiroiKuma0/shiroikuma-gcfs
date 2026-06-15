# CLAUDE.md

Guidance for Claude Code (claude.ai/code) when working in this repository.

## Project overview

**gcfs** — a non-FUSE command-line front-end for **gocryptfs**, built on a fork of
[hardcore-sushi/libgocryptfs](https://github.com/hardcore-sushi/libgocryptfs). It creates,
browses, and incrementally syncs gocryptfs volumes with **no FUSE and no root** — e.g. on an
unrooted Android phone via Termux. Primary use: encrypt-on-write backup of a plaintext tree
to an encrypted volume on a (loseable) USB stick, driven by the `synch` tool.

This repo (`ShiroiKuma0/shiroikuma-gcfs`) is a **fork**: we track upstream and layer the
`gcfs` CLI on top.

## Fork workflow — READ FIRST

- `origin` → `git@github.com:ShiroiKuma0/shiroikuma-gcfs` — our fork (push here).
- `upstream` → `https://github.com/hardcore-sushi/libgocryptfs.git` — original (read-only).
- **`master`** mirrors upstream's default branch (`libgocryptfs`); we do **not** develop on it.
- **`custom`** is our development branch — **all our work lives here** (default branch).
- **Never `git commit`/`git push` unprompted** — only when 白い熊 says **"Push"** (which means
  commit-and-push to `origin custom`). **Never add a `Co-Authored-By: Claude` / Anthropic
  trailer** to commits (global rule).

## What makes this a fork (our additions — all in `package main`)

| File | Role |
| --- | --- |
| `cli.go` | CLI dispatch: `create`/`ls`/`cat`/`put`/`mkdir`/`stat`/`rm`/`sync` + a Go-native `listDir` |
| `sync.go` | incremental size+mtime mirror (encrypt/decrypt), `-base`, `-delete`, `-n`, `-checksum` |
| `common_ops.go` | upstream file + our `gcf_set_mtime` (preserve plaintext mtime on the ciphertext file) |
| `main.go` | entry point delegated to `cli.go` |
| `build-termux.sh` | cross-compile a static-OpenSSL `aarch64` Termux binary |
| `.claude/`, `docs/`, `GCFS.md` | project tooling, test scripts, build/usage notes |

## Architecture / key facts (gotchas)

- The crypto engine is upstream libgocryptfs: `gcf_*` functions in `package main` (cgo
  `//export`, **also plain Go functions**). `cli.go`/`sync.go` call them directly — no FFI.
- **In-volume paths are ABSOLUTE** (`/sub/file`); the root base case is `"/"`. A *relative*
  path infinite-loops in `prepareAtSyscall` (`helpers.go`). Always pass a leading slash.
- **Build requires cgo + OpenSSL** (`stupidgcm`). Pure-Go is NOT available (no `without_openssl`
  stub; `allocator/` uses `C.malloc`). OpenSSL links via `pkg-config`.
- `gcfs sync` **auto-detects direction**: the side containing `gocryptfs.conf` is the volume.
  plain→vol encrypts, vol→plain decrypts.
- gcfs **preserves mtime** (`gcf_set_mtime`), so its volumes are mtime-consistent with a
  desktop `gocryptfs` mount + `rsync -t`. Sync compares by **size + mtime within a window**
  (default 2 s, absorbs exFAT rounding) — like rsync. `-checksum` switches to SHA-256.

## Build

- **Termux / arm64:** `ANDROID_NDK_HOME=$HOME/android-sdk/ndk/28.1.13356709 ./build-termux.sh`
  → `./gcfs-arm64` (PIE `aarch64`, static OpenSSL, only `liblog`/`libdl`/`libc`). First run
  fetches the pinned OpenSSL commit **by SHA** (`.gitmodules` URL + the gitlink SHA) and builds
  it; later runs skip that. Run with the command sandbox disabled (NDK exec, build cache,
  git fetch).
- **Host (testing):** `CGO_ENABLED=1 go build -o gcfs .` (links system `libcrypto`).
- Go toolchain: `~/goroot126/go`. NDK: `28.1.13356709`.

## Test

- `docs/test-sync.sh` — host functional test; cross-checks by mounting the produced volume with
  upstream `gocryptfs` (needs `/dev/fuse`; sandbox disabled).
- `docs/test.sh` — on-device Termux test (`bash /sdcard/tmp/test.sh`; writes `test-results.txt`).

## Deploy to the phone

`adb push gcfs-arm64 /sdcard/tmp/gcfs` — **always `/sdcard/tmp`, never elsewhere**. Then in
Termux: `cp /sdcard/tmp/gcfs ~/gcfs && chmod +x ~/gcfs` (`/sdcard` is `noexec`). Never
`adb install`.

## Usage

    GOCRYPTFS_PASSWORD=… gcfs create <dir>
    gcfs [-passfile F] sync [-n] [-delete] [-base /p] <SRC> <DST>
    # one side is a volume (has gocryptfs.conf): plain→vol encrypts, vol→plain decrypts.
    # password: -passfile FILE (preferred) or $GOCRYPTFS_PASSWORD.

## Use in a backup pipeline

`gcfs sync` is meant to be driven by a backup/sync tool: on a system that can't FUSE-mount
(e.g. an unrooted phone) it replaces "mount, then rsync" with a single crypto-aware
`gcfs sync`. Volumes it writes stay openable by a desktop `gocryptfs` mount.
