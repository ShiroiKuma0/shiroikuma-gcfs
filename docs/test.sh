#!/usr/bin/env bash
# gcfs on-device test. Run in Termux with:   bash /sdcard/tmp/test.sh
# Echoes each command + its output to the screen AND to the log file below.
# Body runs through `tee`, so the log is fully flushed when it finishes.

LOG=/sdcard/tmp/test-results.txt
USB=/storage/XXXX-XXXX          # <-- your stick's UUID; adjust if different
export GOCRYPTFS_PASSWORD=changeme
G="$HOME/gcfs"
PLAIN="$HOME/plain"
REST="$HOME/restored"
PLAIN2="$HOME/plain2"
REST2="$HOME/restored2"
V="$USB/synctest"

say(){ printf '\n========== %s ==========\n' "$*"; }
run(){ printf '\n$ %s\n' "$*"; eval "$*"; printf '[exit %s]\n' "$?"; }
ckfile(){
  if [ "$(cat "$1" 2>/dev/null)" = "$(cat "$2" 2>/dev/null)" ]; then
    echo "MATCH:    $1  ==  $2"
  else
    echo "MISMATCH: $1  !=  $2"
  fi
}

main(){
  say "environment"
  run "uname -m"
  run "cp /sdcard/tmp/gcfs '$G' && chmod +x '$G'"
  run "'$G' 2>&1 | head -3 || true"

  say "clean previous test state"
  run "rm -rf '$PLAIN' '$REST' '$PLAIN2' '$REST2' '$V'"

  say "build a small plaintext tree"
  run "mkdir -p '$PLAIN/sub'"
  run "printf 'alpha\\n' > '$PLAIN/a.txt'"
  run "printf 'beta — 白い熊\\n' > '$PLAIN/sub/b.txt'"
  run "ls -laR '$PLAIN'"

  say "create encrypted volume on the USB"
  run "'$G' create '$V'"
  run "ls -la '$V'"

  say "ENCRYPT push: plaintext -> volume"
  run "'$G' sync '$PLAIN' '$V'"
  run "'$G' ls '$V' /"
  run "'$G' ls '$V' /sub"
  run "'$G' cat '$V' /sub/b.txt"

  say "incremental (expect 0 new, 0 updated)"
  run "'$G' sync '$PLAIN' '$V'"

  say "dry-run shows the new file but writes nothing; then real run"
  run "echo newfile > '$PLAIN/c.txt'"
  run "'$G' sync -n '$PLAIN' '$V'"
  run "'$G' ls '$V' /          # c.txt should NOT be here yet"
  run "'$G' sync '$PLAIN' '$V'"
  run "'$G' ls '$V' /          # c.txt present now"

  say "DECRYPT restore: volume -> plaintext"
  run "'$G' sync '$V' '$REST'"
  run "ls -laR '$REST'"

  say "round-trip content check (no diff dependency)"
  ckfile "$PLAIN/a.txt"     "$REST/a.txt"
  ckfile "$PLAIN/sub/b.txt" "$REST/sub/b.txt"
  ckfile "$PLAIN/c.txt"     "$REST/c.txt"

  say "-delete mirror: remove a.txt from source, prune volume"
  run "rm '$PLAIN/a.txt'"
  run "'$G' sync -delete '$PLAIN' '$V'"
  run "'$G' ls '$V' /          # a.txt should be gone"

  say "mtime on exFAT: re-sync incremental; mtime-only change detected; preserved"
  run "'$G' sync '$PLAIN' '$V'                              # expect 0 new, 0 updated"
  run "touch -d '2009-02-13 23:31:30' '$PLAIN/sub/b.txt'"
  run "'$G' sync '$PLAIN' '$V'                              # mtime-only change -> 1 updated"
  run "'$G' sync '$PLAIN' '$V'                              # again -> 0 updated (preserved)"

  say "-base scoped round-trip"
  run "mkdir -p '$PLAIN2/x'"
  run "printf 'scoped — 白い熊\\n' > '$PLAIN2/x/s.txt'"
  run "'$G' sync -base /scoped '$PLAIN2' '$V'"
  run "'$G' ls '$V' /scoped/x"
  run "'$G' sync -base /scoped '$V' '$REST2'"
  ckfile "$PLAIN2/x/s.txt" "$REST2/x/s.txt"

  say "ciphertext layout on the USB (encrypted names)"
  run "ls -la '$V'"

  say "DONE"
}

main 2>&1 | tee "$LOG"
