---
name: build
description: Cross-compile the gcfs Termux/arm64 binary via build-termux.sh, then auto-deliver it — run `adb devices` UNSANDBOXED and, if a device is present, `adb push` it to /sdcard/tmp without asking. Use whenever 白い熊 asks to build gcfs, build the binary, make a Termux build, or build and send to the phone.
---

# Build gcfs (Termux / arm64) and auto-deliver to the phone

> **Always build first without asking permission to build.** After a successful build,
> deliver the binary **automatically** — no transfer prompt, no "phone connected?" question.
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

3. **Deliver automatically — never ask.** Run `adb devices` UNSANDBOXED (a sandboxed
   check wrongly reports no device). If a device is present, `adb push gcfs-arm64
   /sdcard/tmp/gcfs` UNSANDBOXED (`adb shell mkdir -p /sdcard/tmp` first) without asking,
   then announce loudly what landed. If no device is present, report that the phone isn't
   connected. (gcfs is a Termux binary, not an APK — the global /after-build and /adb-push
   skills are APK-specific, so deliver inline here.)

4. **If pushed, remind:** in Termux `cp /sdcard/tmp/gcfs ~/gcfs && chmod +x ~/gcfs`
   (`/sdcard` is mounted `noexec`).
