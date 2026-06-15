package main

// gcfs find: emit per-file metadata for a gocryptfs volume in the exact format
// synch's pc_find produces (`find -type f -printf '%P\t%T@\t%TY-%Tm-%Td %TH:%TM\t%s\n'`),
// so `compare_files`/`missing_files` can diff an encrypted leg against a plaintext one.
// Lines: <relpath>\t<mtime-epoch>\t<YYYY-MM-DD HH:MM>\t<plaintext-size>, files only,
// paths relative to -base (default "/"), no leading slash.

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

func doFind(passfile, base, volDir string) {
	vid := openVol(volDir, getPassword(passfile))
	defer gcf_close(vid)
	base = cleanVol(base)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	var walk func(string)
	walk = func(vdir string) {
		entries, err := listDir(vid, vdir)
		if err != nil {
			return
		}
		for _, e := range entries {
			vpath := vjoin(vdir, e.Name)
			mode, size, mtime, ok := gcf_get_attrs(vid, vpath)
			if !ok {
				continue
			}
			if isDirMode(mode) {
				walk(vpath)
				continue
			}
			if mode&syscall.S_IFMT != syscall.S_IFREG {
				continue // -type f: regular files only
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(vpath, base), "/")
			date := time.Unix(int64(mtime), 0).Format("2006-01-02 15:04")
			fmt.Fprintf(w, "%s\t%d\t%s\t%d\n", rel, mtime, date, size)
		}
	}
	walk(base)
}
