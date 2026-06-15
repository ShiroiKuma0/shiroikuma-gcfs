#!/bin/bash
# Functional test for `gcfs sync` on the host (amd64), cross-checked against
# upstream gocryptfs via FUSE.
set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
export GOCRYPTFS_PASSWORD='changeme'
T=$(mktemp -d); trap 'fusermount -u "$T/mnt" 2>/dev/null; rm -rf "$T"' EXIT
G="$T/gcfs"; ( cd "$REPO" && CGO_ENABLED=1 go build -o "$G" . ) || { echo "go build failed" >&2; exit 1; }
SRC="$T/src"; VOL="$T/vol"; REST="$T/restored"; MNT="$T/mnt"; PW="$T/pw"
printf '%s' "$GOCRYPTFS_PASSWORD" > "$PW"

pass=0; fail=0
ck(){ if [ "$1" = "$2" ]; then echo "  PASS: $3"; pass=$((pass+1)); else echo "  FAIL: $3 (got [$1] want [$2])"; fail=$((fail+1)); fi; }
mount_vol(){ mkdir -p "$MNT"; gocryptfs -q -passfile "$PW" "$VOL" "$MNT"; }
umount_vol(){ fusermount -u "$MNT"; }

# --- build source tree ---
mkdir -p "$SRC/a/b" "$SRC/empty"
printf 'file one\n'            > "$SRC/a/one.txt"     # 9 bytes
printf 'nested two — 白い熊\n' > "$SRC/a/b/two.txt"
head -c 5000 /dev/urandom      > "$SRC/big.bin"
printf 'top\n'                 > "$SRC/top.txt"

echo "=== 1. encrypt ==="
"$G" create "$VOL"
"$G" sync "$SRC" "$VOL"
mount_vol
diff -r "$MNT" "$SRC" >/dev/null 2>&1; ck "$?" "0" "mounted volume matches source after encrypt"
umount_vol

echo "=== 2. incremental (no changes) → all unchanged ==="
out=$("$G" sync "$SRC" "$VOL" 2>&1); echo "  $out"
echo "$out" | grep -q "0 new, 0 updated"; ck "$?" "0" "second run reports nothing new/updated"

echo "=== 3. same-size content change: size-only misses, -checksum catches ==="
printf 'file ONE\n' > "$SRC/a/one.txt"   # still 9 bytes
"$G" sync "$SRC" "$VOL" >/dev/null 2>&1   # size-only: should skip
mount_vol; got=$(cat "$MNT/a/one.txt"); umount_vol
ck "$got" "file one" "size-only sync does NOT see same-size edit (expected limitation)"
"$G" sync -checksum "$SRC" "$VOL" >/dev/null 2>&1
mount_vol; got=$(cat "$MNT/a/one.txt"); umount_vol
ck "$got" "file ONE" "-checksum sync DOES update same-size edit"

echo "=== 4. shrink a file → no stale tail ==="
printf 'x\n' > "$SRC/big.bin"
"$G" sync "$SRC" "$VOL" >/dev/null 2>&1
mount_vol; got=$(cat "$MNT/big.bin"); sz=$(stat -c%s "$MNT/big.bin"); umount_vol
ck "$got" "x" "shrunk file content correct"
ck "$sz" "2" "shrunk file size correct (no stale tail)"

echo "=== 5. -delete prunes removed file ==="
rm "$SRC/top.txt"
"$G" sync -delete "$SRC" "$VOL" >/dev/null 2>&1
mount_vol; test ! -e "$MNT/top.txt"; ck "$?" "0" "deleted file is gone from volume"; umount_vol

echo "=== 6. decrypt round-trip matches source ==="
"$G" sync "$VOL" "$REST" >/dev/null 2>&1
diff -r "$REST" "$SRC" >/dev/null 2>&1; ck "$?" "0" "decrypted tree matches source"

echo "=== 7. dry-run makes no changes ==="
printf 'phantom\n' > "$SRC/phantom.txt"
out=$("$G" sync -n "$SRC" "$VOL" 2>&1)
echo "$out" | grep -q "phantom.txt"; ck "$?" "0" "dry-run lists the new file"
mount_vol; test ! -e "$MNT/phantom.txt"; ck "$?" "0" "dry-run did NOT write it"; umount_vol

echo "=== 8. mtime preserved on encrypt + mtime-only change detected ==="
"$G" sync "$SRC" "$VOL" >/dev/null 2>&1            # normalise
smt=$(stat -c %Y "$SRC/a/b/two.txt")
mount_vol; vmt=$(stat -c %Y "$MNT/a/b/two.txt"); umount_vol
dd=$((smt - vmt)); dd=${dd#-}
ck "$([ "$dd" -le 2 ] && echo ok)" "ok" "encrypt preserves mtime (Δ=${dd}s)"
touch -d '2009-02-13 23:31:30' "$SRC/a/b/two.txt"  # change mtime only, same content/size
out=$("$G" sync "$SRC" "$VOL" 2>&1); echo "  $out"
echo "$out" | grep -q "1 updated"; ck "$?" "0" "mtime-only change detected as updated"
"$G" sync "$SRC" "$VOL" >/dev/null 2>&1            # and now back to 0
out=$("$G" sync "$SRC" "$VOL" 2>&1)
echo "$out" | grep -q "0 new, 0 updated"; ck "$?" "0" "re-sync after update is incremental (mtime preserved)"

echo "=== 9. -base scoped round-trip ==="
S2="$T/src2"; R2="$T/restored2"
mkdir -p "$S2/x"; printf 'scoped — 白い熊\n' > "$S2/x/s.txt"
"$G" sync -base /scoped "$S2" "$VOL" >/dev/null 2>&1
"$G" ls "$VOL" /scoped/x | grep -q 's.txt'; ck "$?" "0" "scoped file lands at /scoped/x in volume"
"$G" sync -base /scoped "$VOL" "$R2" >/dev/null 2>&1
ck "$(cat "$S2/x/s.txt" 2>/dev/null)" "$(cat "$R2/x/s.txt" 2>/dev/null)" "-base decrypt round-trip content matches"

echo
echo "RESULT: $pass passed, $fail failed"
rm -rf "$T"
