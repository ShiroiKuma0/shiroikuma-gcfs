---
name: build
description: Cross-compile the gcfs Termux/arm64 binary via build-termux.sh, then ask whether to adb push it to the phone at /sdcard/tmp. Use whenever 白い熊 asks to build gcfs, build the binary, make a Termux build, or build and send to the phone.
---

# Build gcfs (Termux / arm64) and optionally push to the phone

> **Always build first without asking permission to build.** The only question is the
> transfer one, asked **after** a successful build.
> **The adb push destination is ALWAYS `/sdcard/tmp`** — never `/sdcard/Download` or elsewhere.
> **Never `git commit`/`git push` here, and never `adb install`.** The user copies/installs
> on the phone themselves.

## Steps

1. **Build** (sandbox disabled — NDK exec, build cache, git fetch):
   ```bash
   ANDROID_NDK_HOME=$HOME/android-sdk/ndk/28.1.13356709 ./build-termux.sh
   ```
   Produces `./gcfs-arm64` (PIE `aarch64`, static OpenSSL, bionic-only deps). The first run
   fetches the pinned OpenSSL commit by SHA and builds it (~minutes); later runs skip it.

2. **Sanity-check:** `file gcfs-arm64` (ELF `aarch64`) and
   `"$ANDROID_NDK_HOME"/toolchains/llvm/prebuilt/linux-x86_64/bin/llvm-readelf -d gcfs-arm64 | grep NEEDED`
   (must be only `liblog`/`libdl`/`libc`).

3. **Ask via `AskUserQuestion`** whether to `adb push gcfs-arm64 /sdcard/tmp/gcfs`
   (`adb shell mkdir -p /sdcard/tmp` first; adb needs the sandbox disabled — USB).

4. **If pushed, remind:** in Termux `cp /sdcard/tmp/gcfs ~/gcfs && chmod +x ~/gcfs`
   (`/sdcard` is mounted `noexec`).
