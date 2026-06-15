package main

// gcfs sync: incremental one-way mirror between a plaintext directory tree and
// a gocryptfs volume, with no FUSE. Direction is inferred from which side is a
// volume (contains gocryptfs.conf):
//
//   gcfs sync <plaindir> <volume>   → ENCRYPT (push plaintext into the volume)
//   gcfs sync <volume>   <plaindir> → DECRYPT (restore plaintext from the volume)
//
// Comparison is by size + mtime within a modify-window (like rsync/zaloha; the
// engine reports the decrypted size and the file mtime, which gcfs preserves on
// write so phone- and desktop-written volumes stay mtime-consistent). -checksum
// switches to SHA-256 content comparison. -base offsets the in-volume path so a
// subtree can be synced into a sub-path of the volume (JD scoping). -delete
// prunes extraneous destination entries. -n is a dry run. -v lists every action.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type syncOpts struct {
	base     string // in-volume path the plaintext tree maps to (default "/")
	window   int64  // mtime tolerance in seconds (absorbs exFAT/FAT rounding)
	delete   bool
	dryRun   bool
	checksum bool
	verbose  bool
}

func isVolume(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "gocryptfs.conf"))
	return err == nil
}

func vjoin(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

// cleanVol normalises an in-volume path to a clean absolute slash-path.
func cleanVol(p string) string {
	return path.Clean("/" + strings.Trim(filepath.ToSlash(p), "/"))
}

func dryTag(o syncOpts) string {
	if o.dryRun {
		return " (dry-run)"
	}
	return ""
}

func isDirMode(mode uint32) bool { return mode&syscall.S_IFMT == syscall.S_IFDIR }

// withinWindow reports whether two epoch-second times are within w seconds.
func withinWindow(a uint64, b, w int64) bool {
	d := int64(a) - b
	if d < 0 {
		d = -d
	}
	return d <= w
}

func doSync(passfile string, o syncOpts, src, dst string) {
	o.base = cleanVol(o.base)
	srcVol, dstVol := isVolume(src), isVolume(dst)
	switch {
	case dstVol && !srcVol:
		encryptSync(passfile, o, src, dst)
	case srcVol && !dstVol:
		decryptSync(passfile, o, src, dst)
	case srcVol && dstVol:
		die("both SRC and DST look like gocryptfs volumes; exactly one side must be a plaintext dir")
	default:
		die("neither SRC nor DST is a gocryptfs volume (no gocryptfs.conf) — create one with `gcfs create`")
	}
}

// ---------- ENCRYPT (plaintext dir → volume[:base]) ----------

func encryptSync(passfile string, o syncOpts, srcDir, volDir string) {
	vid := openVol(volDir, getPassword(passfile))
	defer gcf_close(vid)
	if o.base != "/" && !o.dryRun {
		mkdirAll(vid, o.base)
	}
	var nNew, nUpd, nSkip, nDir int
	seen := map[string]bool{} // in-volume paths present in source

	err := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip (unreadable): %s: %v\n", p, err)
			return nil
		}
		rel, _ := filepath.Rel(srcDir, p)
		if rel == "." {
			return nil
		}
		vpath := cleanVol(o.base + "/" + filepath.ToSlash(rel))
		seen[vpath] = true

		if d.IsDir() {
			nDir++
			if !o.dryRun {
				mkdirAll(vid, vpath)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			fmt.Fprintf(os.Stderr, "skip (not a regular file — symlinks unsupported): %s\n", rel)
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			fmt.Fprintf(os.Stderr, "skip (stat failed): %s: %v\n", rel, ierr)
			return nil
		}
		srcSize := uint64(info.Size())
		srcMtime := info.ModTime().Unix()
		_, dSize, dMtime, exists := gcf_get_attrs(vid, vpath)
		need := true
		if exists {
			if o.checksum {
				need = dSize != srcSize || sha256File(p) != sha256Vol(vid, vpath)
			} else {
				need = !(dSize == srcSize && withinWindow(dMtime, srcMtime, o.window))
			}
		}
		if !need {
			nSkip++
			if o.verbose {
				fmt.Printf("= %s\n", rel)
			}
			return nil
		}
		if exists {
			nUpd++
		} else {
			nNew++
		}
		if o.verbose || o.dryRun {
			mark := "+"
			if exists {
				mark = "~"
			}
			fmt.Printf("%s %s\n", mark, rel)
		}
		if !o.dryRun {
			encryptFile(vid, vpath, p)
			gcf_set_mtime(vid, vpath, srcMtime) // preserve plaintext mtime
		}
		return nil
	})
	if err != nil {
		die("walk %s: %v", srcDir, err)
	}

	nDel := 0
	if o.delete {
		nDel = pruneVolume(vid, o.base, seen, o)
	}
	fmt.Fprintf(os.Stderr, "encrypt %s → %s%s: %d new, %d updated, %d unchanged, %d dirs, %d deleted%s\n",
		srcDir, volDir, baseTag(o.base), nNew, nUpd, nSkip, nDir, nDel, dryTag(o))
}

func encryptFile(vid int, vpath, srcPath string) {
	mkdirAll(vid, path.Dir(vpath))
	f, err := os.Open(srcPath)
	if err != nil {
		die("open %s: %v", srcPath, err)
	}
	defer f.Close()
	fh := gcf_open_write_mode(vid, vpath, 0o600)
	if fh < 0 {
		die("create %s in volume", vpath)
	}
	gcf_truncate(vid, vpath, 0) // drop any stale tail before rewriting
	buf := make([]byte, chunk)
	var off uint64
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if w := gcf_write_file(vid, fh, off, buf[:n]); int(w) != n {
				gcf_close_file(vid, fh)
				die("short write to %s (%d of %d)", vpath, w, n)
			}
			off += uint64(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			gcf_close_file(vid, fh)
			die("read %s: %v", srcPath, rerr)
		}
	}
	gcf_close_file(vid, fh)
}

// pruneVolume removes volume entries under `dir` whose in-volume path is not in `seen`.
func pruneVolume(vid int, dir string, seen map[string]bool, o syncOpts) int {
	entries, err := listDir(vid, dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		vpath := vjoin(dir, e.Name)
		if isDirMode(e.Mode) {
			n += pruneVolume(vid, vpath, seen, o) // empty it first
			if !seen[vpath] {
				if o.verbose || o.dryRun {
					fmt.Printf("- %s/\n", vpath)
				}
				if !o.dryRun {
					gcf_rmdir(vid, vpath)
				}
				n++
			}
		} else if !seen[vpath] {
			if o.verbose || o.dryRun {
				fmt.Printf("- %s\n", vpath)
			}
			if !o.dryRun {
				gcf_remove_file(vid, vpath)
			}
			n++
		}
	}
	return n
}

// ---------- DECRYPT (volume[:base] → plaintext dir) ----------

func decryptSync(passfile string, o syncOpts, volDir, dstDir string) {
	vid := openVol(volDir, getPassword(passfile))
	defer gcf_close(vid)
	if !o.dryRun {
		os.MkdirAll(dstDir, 0o700)
	}
	var nNew, nUpd, nSkip, nDir int
	seen := map[string]bool{} // dst-relative paths present in volume

	var walk func(vdir string)
	walk = func(vdir string) {
		entries, err := listDir(vid, vdir)
		if err != nil {
			die("list %s: %v", vdir, err)
		}
		for _, e := range entries {
			vpath := vjoin(vdir, e.Name)
			rel := strings.TrimPrefix(strings.TrimPrefix(vpath, o.base), "/")
			if rel == "" {
				continue
			}
			seen[rel] = true
			dstPath := filepath.Join(dstDir, filepath.FromSlash(rel))
			if isDirMode(e.Mode) {
				nDir++
				if !o.dryRun {
					os.MkdirAll(dstPath, 0o700)
				}
				walk(vpath)
				continue
			}
			_, vSize, vMtime, _ := gcf_get_attrs(vid, vpath)
			exists := false
			need := true
			if st, serr := os.Stat(dstPath); serr == nil {
				exists = true
				if o.checksum {
					need = uint64(st.Size()) != vSize || sha256File(dstPath) != sha256Vol(vid, vpath)
				} else {
					need = !(uint64(st.Size()) == vSize && withinWindow(vMtime, st.ModTime().Unix(), o.window))
				}
			}
			if !need {
				nSkip++
				if o.verbose {
					fmt.Printf("= %s\n", rel)
				}
				continue
			}
			if exists {
				nUpd++
			} else {
				nNew++
			}
			if o.verbose || o.dryRun {
				mark := "+"
				if exists {
					mark = "~"
				}
				fmt.Printf("%s %s\n", mark, rel)
			}
			if !o.dryRun {
				decryptFileToDisk(vid, vpath, dstPath)
				t := time.Unix(int64(vMtime), 0)
				os.Chtimes(dstPath, t, t) // preserve plaintext mtime
			}
		}
	}
	walk(o.base)

	nDel := 0
	if o.delete {
		nDel = pruneDisk(dstDir, dstDir, seen, o)
	}
	fmt.Fprintf(os.Stderr, "decrypt %s%s → %s: %d new, %d updated, %d unchanged, %d dirs, %d deleted%s\n",
		volDir, baseTag(o.base), dstDir, nNew, nUpd, nSkip, nDir, nDel, dryTag(o))
}

func decryptFileToDisk(vid int, vpath, dstPath string) {
	os.MkdirAll(filepath.Dir(dstPath), 0o700)
	fh := gcf_open_read_mode(vid, vpath)
	if fh < 0 {
		die("open %s in volume", vpath)
	}
	defer gcf_close_file(vid, fh)
	out, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		die("create %s: %v", dstPath, err)
	}
	defer out.Close()
	buf := make([]byte, chunk)
	var off uint64
	for {
		n := gcf_read_file(vid, fh, off, buf)
		if n == 0 {
			break
		}
		if _, werr := out.Write(buf[:n]); werr != nil {
			die("write %s: %v", dstPath, werr)
		}
		off += uint64(n)
	}
}

// pruneDisk removes files/dirs under root whose path (relative to base) is not in `seen`.
func pruneDisk(root, base string, seen map[string]bool, o syncOpts) int {
	n := 0
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		full := filepath.Join(root, e.Name())
		rel, _ := filepath.Rel(base, full)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			n += pruneDisk(full, base, seen, o)
			if !seen[rel] {
				if o.verbose || o.dryRun {
					fmt.Printf("- %s/\n", rel)
				}
				if !o.dryRun {
					os.Remove(full) // only succeeds if now empty
				}
				n++
			}
		} else if !seen[rel] {
			if o.verbose || o.dryRun {
				fmt.Printf("- %s\n", rel)
			}
			if !o.dryRun {
				os.Remove(full)
			}
			n++
		}
	}
	return n
}

func baseTag(base string) string {
	if base == "/" {
		return ""
	}
	return ":" + base
}

// ---------- hashing helpers (only used with -checksum) ----------

func sha256File(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sha256Vol(vid int, vpath string) string {
	fh := gcf_open_read_mode(vid, vpath)
	if fh < 0 {
		return ""
	}
	defer gcf_close_file(vid, fh)
	h := sha256.New()
	buf := make([]byte, chunk)
	var off uint64
	for {
		n := gcf_read_file(vid, fh, off, buf)
		if n == 0 {
			break
		}
		h.Write(buf[:n])
		off += uint64(n)
	}
	return hex.EncodeToString(h.Sum(nil))
}
