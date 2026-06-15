---
name: upstream-sync
description: Merge new upstream commits from hardcore-sushi/libgocryptfs into our custom branch. Use when 白い熊 says upstream libgocryptfs has changes, asks to sync/update to upstream, or to rebase custom onto upstream.
---

# Sync the fork with upstream libgocryptfs

> **Never `git push`/`commit` unprompted; only when 白い熊 says "Push".** No Claude commit trailer.

Model: `master` mirrors `upstream/libgocryptfs`; `custom` holds our gcfs additions (`cli.go`,
`sync.go`, the `common_ops.go` `gcf_set_mtime` change, `main.go`, `build-termux.sh`,
`.claude/`, `docs/`, `GCFS.md`). Upstream libgocryptfs is low-traffic (pinned OpenSSL), so
updates are rare.

## Steps

1. The clone may be shallow — `git fetch --unshallow upstream || git fetch upstream`.
2. Update master: `git checkout master && git merge --ff-only upstream/libgocryptfs`.
3. Replay our work: `git checkout custom && git rebase master`. Conflicts will be in the
   files we touch — `cli.go`/`sync.go` are ours (new); `common_ops.go`/`main.go` are upstream
   files with our edits.
4. **Re-verify the build:** `./build-termux.sh` (or host `CGO_ENABLED=1 go build -o gcfs .`),
   then run `docs/test-sync.sh`.
5. **Stop and let 白い熊 test.** Push only on "Push": `git push origin custom master`.
