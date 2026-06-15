package main

// gcfs: a non-FUSE command-line front-end over the libgocryptfs engine.
// Reuses the proven gcf_* functions directly (no FFI). In-volume paths are
// always absolute ("/sub/file"); the engine's root base case is "/".

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"

	"libgocryptfs/v2/internal/configfile"
	"libgocryptfs/v2/internal/nametransform"
	"libgocryptfs/v2/internal/syscallcompat"
)

const chunk = 64 * 1024

func die(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "gcfs: "+format+"\n", a...)
	os.Exit(1)
}

// getPassword: -passfile FILE, else $GOCRYPTFS_PASSWORD. (stdin is reserved for
// file data in `put`, so we never read the password from it.)
func getPassword(passfile string) []byte {
	if passfile != "" {
		b, err := os.ReadFile(passfile)
		if err != nil {
			die("reading passfile: %v", err)
		}
		return []byte(strings.TrimRight(string(b), "\r\n"))
	}
	if p := os.Getenv("GOCRYPTFS_PASSWORD"); p != "" {
		return []byte(p)
	}
	die("no password: set $GOCRYPTFS_PASSWORD or pass -passfile FILE")
	return nil
}

// open an existing volume, return volumeID (fresh password copy each call,
// because gcf_init wipes it).
func openVol(cipherDir string, pw []byte) int {
	cp := append([]byte(nil), pw...)
	vid := gcf_init(cipherDir, cp, nil, nil)
	if vid < 0 {
		die("cannot open volume %q (wrong password or not a gocryptfs dir): code %d", cipherDir, vid)
	}
	return vid
}

// dirEntry is the Go-native equivalent of what gcf_list_dir C-packs.
type dirEntry struct {
	Name string
	Mode uint32
}

// listDir mirrors gcf_list_dir's logic but returns Go values.
func listDir(volumeID int, dirName string) ([]dirEntry, error) {
	value, ok := OpenedVolumes.Load(volumeID)
	if !ok {
		return nil, fmt.Errorf("bad volume id")
	}
	volume := value.(*Volume)
	parentDirFd, cDirName, err := volume.prepareAtSyscallMyself(dirName)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(parentDirFd)
	fd, err := syscallcompat.Openat(parentDirFd, cDirName, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	cipherEntries, err := syscallcompat.Getdents(fd)
	if err != nil {
		return nil, err
	}
	var cachedIV []byte
	if !volume.plainTextNames {
		cachedIV, err = volume.nameTransform.ReadDirIVAt(fd)
		if err != nil {
			return nil, err
		}
	}
	var out []dirEntry
	for i := range cipherEntries {
		cName := cipherEntries[i].Name
		if dirName == "/" && cName == configfile.ConfDefaultName {
			continue
		}
		if volume.plainTextNames {
			out = append(out, dirEntry{cName, cipherEntries[i].Mode})
			continue
		}
		if cName == nametransform.DirIVFilename {
			continue
		}
		switch nametransform.NameType(cName) {
		case nametransform.LongNameContent:
			cNameLong, err := nametransform.ReadLongNameAt(fd, cName)
			if err != nil {
				continue
			}
			cName = cNameLong
		case nametransform.LongNameFilename:
			continue
		}
		name, err := volume.nameTransform.DecryptName(cName, cachedIV)
		if err != nil {
			continue
		}
		out = append(out, dirEntry{name, cipherEntries[i].Mode})
	}
	return out, nil
}

// mkdirAll: mkdir -p for an absolute in-volume path.
func mkdirAll(vid int, p string) {
	p = path.Clean("/" + p)
	if p == "/" {
		return
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	cur := ""
	for _, part := range parts {
		cur += "/" + part
		if _, _, _, ok := gcf_get_attrs(vid, cur); ok {
			continue // already exists
		}
		if !gcf_mkdir(vid, cur, 0o700) {
			die("mkdir %q failed", cur)
		}
	}
}

func main() {
	args := os.Args[1:]
	passfile := ""
	// pull out -passfile FILE (anywhere) before positional parsing
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-passfile" && i+1 < len(args):
			passfile = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-passfile="):
			passfile = strings.TrimPrefix(args[i], "-passfile=")
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, `gcfs — non-FUSE gocryptfs CLI
usage:
  gcfs [-passfile F] create <cipherdir>
  gcfs [-passfile F] ls     <cipherdir> <volpath>
  gcfs [-passfile F] stat   <cipherdir> <volpath>
  gcfs [-passfile F] cat    <cipherdir> <volpath>
  gcfs [-passfile F] put    <cipherdir> <volpath>     # data from stdin
  gcfs [-passfile F] mkdir  <cipherdir> <volpath>
  gcfs [-passfile F] rm     <cipherdir> <volpath>
  gcfs [-passfile F] sync   [-n] [-delete] [-checksum] [-v] [-base /p] <SRC> <DST>
                            # one of SRC/DST is a volume; the other plaintext.
                            # plain→vol encrypts, vol→plain decrypts (size+mtime).
  gcfs [-passfile F] find   [-base /p] <volume>
                            # path<TAB>mtime<TAB>date<TAB>size, files only (pc_find format)
password: -passfile F or $GOCRYPTFS_PASSWORD`)
		os.Exit(2)
	}

	if rest[0] == "sync" {
		o := syncOpts{window: 2}
		var pos []string
		for i := 1; i < len(rest); i++ {
			a := rest[i]
			switch {
			case a == "-n" || a == "--dry-run":
				o.dryRun = true
			case a == "-delete" || a == "--delete":
				o.delete = true
			case a == "-checksum" || a == "--checksum":
				o.checksum = true
			case a == "-v" || a == "--verbose":
				o.verbose = true
			case a == "-base" && i+1 < len(rest):
				o.base = rest[i+1]
				i++
			case strings.HasPrefix(a, "-base="):
				o.base = strings.TrimPrefix(a, "-base=")
			default:
				if strings.HasPrefix(a, "-") {
					die("sync: unknown flag %q", a)
				}
				pos = append(pos, a)
			}
		}
		if len(pos) != 2 {
			die("usage: gcfs [-passfile F] sync [-n] [-delete] [-checksum] [-v] [-base /p] <SRC> <DST>")
		}
		doSync(passfile, o, pos[0], pos[1])
		return
	}

	if rest[0] == "find" {
		base := "/"
		var pos []string
		for i := 1; i < len(rest); i++ {
			a := rest[i]
			switch {
			case a == "-base" && i+1 < len(rest):
				base = rest[i+1]
				i++
			case strings.HasPrefix(a, "-base="):
				base = strings.TrimPrefix(a, "-base=")
			default:
				if strings.HasPrefix(a, "-") {
					die("find: unknown flag %q", a)
				}
				pos = append(pos, a)
			}
		}
		if len(pos) != 1 {
			die("usage: gcfs [-passfile F] find [-base /p] <volume>")
		}
		doFind(passfile, base, pos[0])
		return
	}

	cmd, cipherDir := rest[0], rest[1]
	arg := func(i int) string {
		if len(rest) <= i {
			die("%s: missing argument", cmd)
		}
		return rest[i]
	}
	vpath := func() string { return path.Clean("/" + strings.TrimPrefix(arg(2), "/")) }

	switch cmd {
	case "create":
		if err := os.MkdirAll(cipherDir, 0o700); err != nil {
			die("%v", err)
		}
		pw := getPassword(passfile)
		if !gcf_create_volume(cipherDir, append([]byte(nil), pw...), false, -1, 16, "gcfs", nil) {
			die("create_volume failed (dir must be empty)")
		}
		// sanity: open it
		vid := openVol(cipherDir, pw)
		gcf_close(vid)
		fmt.Fprintf(os.Stderr, "created gocryptfs volume at %s\n", cipherDir)

	case "ls":
		vid := openVol(cipherDir, getPassword(passfile))
		defer gcf_close(vid)
		entries, err := listDir(vid, vpath())
		if err != nil {
			die("ls %q: %v", vpath(), err)
		}
		for _, e := range entries {
			t := "f"
			if e.Mode&syscall.S_IFMT == syscall.S_IFDIR {
				t = "d"
			}
			fmt.Printf("%s %s\n", t, e.Name)
		}

	case "stat":
		vid := openVol(cipherDir, getPassword(passfile))
		defer gcf_close(vid)
		mode, size, mtime, ok := gcf_get_attrs(vid, vpath())
		if !ok {
			die("stat %q: not found", vpath())
		}
		fmt.Printf("path=%s mode=%o size=%d mtime=%d\n", vpath(), mode, size, mtime)

	case "cat":
		vid := openVol(cipherDir, getPassword(passfile))
		defer gcf_close(vid)
		fh := gcf_open_read_mode(vid, vpath())
		if fh < 0 {
			die("cat %q: cannot open", vpath())
		}
		buf := make([]byte, chunk)
		var off uint64
		w := bufio.NewWriter(os.Stdout)
		for {
			n := gcf_read_file(vid, fh, off, buf)
			if n == 0 {
				break
			}
			w.Write(buf[:n])
			off += uint64(n)
		}
		w.Flush()
		gcf_close_file(vid, fh)

	case "put":
		vid := openVol(cipherDir, getPassword(passfile))
		defer gcf_close(vid)
		dst := vpath()
		mkdirAll(vid, path.Dir(dst))
		fh := gcf_open_write_mode(vid, dst, 0o600)
		if fh < 0 {
			die("put %q: cannot create", dst)
		}
		buf := make([]byte, chunk)
		var off uint64
		r := bufio.NewReader(os.Stdin)
		for {
			n, rerr := io.ReadFull(r, buf)
			if n > 0 {
				if w := gcf_write_file(vid, fh, off, buf[:n]); int(w) != n {
					gcf_close_file(vid, fh)
					die("put %q: short write (%d of %d)", dst, w, n)
				}
				off += uint64(n)
			}
			if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
				break
			}
			if rerr != nil {
				gcf_close_file(vid, fh)
				die("put %q: read stdin: %v", dst, rerr)
			}
		}
		gcf_close_file(vid, fh)
		fmt.Fprintf(os.Stderr, "wrote %d bytes to %s\n", off, dst)

	case "mkdir":
		vid := openVol(cipherDir, getPassword(passfile))
		defer gcf_close(vid)
		mkdirAll(vid, vpath())

	case "rm":
		vid := openVol(cipherDir, getPassword(passfile))
		defer gcf_close(vid)
		if !gcf_remove_file(vid, vpath()) {
			die("rm %q failed", vpath())
		}

	default:
		die("unknown command %q", cmd)
	}
}
