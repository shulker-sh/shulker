package saves

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fsutil"
)

const commentFormat = 1

var now = time.Now

// Source is where a backup's worlds come from. Only, when set, narrows it to the worlds named;
// the rest is what the zip comment records.
type Source struct {
	Dir           string
	Only          []string
	Instance      string
	Minecraft     string
	Loader        string
	LoaderVersion string
}

// Home is the folder backups go in. A shared one is a save group's, where the filename also names
// the instance that took the backup.
type Home struct {
	Dir    string
	Shared bool
}

// comment is the zip comment, the backup's record. Worlds is left out when the names would not fit.
type comment struct {
	Format        int      `json:"format"`
	Taken         string   `json:"taken"`
	Reason        string   `json:"reason"`
	Instance      string   `json:"instance,omitempty"`
	WorldCount    int      `json:"worldCount"`
	Worlds        []string `json:"worlds,omitempty"`
	Minecraft     string   `json:"minecraft,omitempty"`
	Loader        string   `json:"loader,omitempty"`
	LoaderVersion string   `json:"loaderVersion,omitempty"`
}

// commentFor is c as a zip comment. One too long for the format's 16-bit length drops the world
// names, which the zip's entries still hold, and one too long even then is left off, never cut.
func commentFor(c comment) string {
	if meta, _ := json.Marshal(c); len(meta) <= math.MaxUint16 {
		return string(meta)
	}
	c.Worlds = nil
	if meta, _ := json.Marshal(c); len(meta) <= math.MaxUint16 {
		return string(meta)
	}
	return ""
}

// Take zips src's worlds into a new backup in home, calling each before it zips a world with whether
// a running game has it open, which zips anyway but may be torn. With no worlds to zip it writes
// nothing and returns a zero Backup rather than failing, so a caller backing up on the way past
// something else can carry on.
func Take(src Source, home Home, reason string, each func(world string, open bool)) (Backup, error) {
	worlds, err := Worlds(src.Dir)
	if err != nil {
		return Backup{}, err
	}
	if src.Only != nil {
		worlds = slices.DeleteFunc(worlds, func(w string) bool { return !slices.Contains(src.Only, w) })
	}
	if len(worlds) == 0 {
		return Backup{}, nil
	}
	if err := os.MkdirAll(home.Dir, 0o755); err != nil {
		return Backup{}, err
	}
	at := now()
	seq, err := nextSeq(home.Dir, at)
	if err != nil {
		return Backup{}, err
	}
	id := at.Format(timeLayout)
	if seq > 1 {
		id += "-" + strconv.Itoa(seq)
	}
	if home.Shared && src.Instance != "" {
		id += "-" + src.Instance
	}
	id += "-" + reason
	meta := commentFor(comment{
		Format:        commentFormat,
		Taken:         at.UTC().Format(time.RFC3339),
		Reason:        reason,
		Instance:      src.Instance,
		WorldCount:    len(worlds),
		Worlds:        worlds,
		Minecraft:     src.Minecraft,
		Loader:        src.Loader,
		LoaderVersion: src.LoaderVersion,
	})
	path := filepath.Join(home.Dir, id+".zip")
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(writeZip(pw, src.Dir, worlds, meta, each)) }()
	if err := fsutil.WriteFrom(path, pr); err != nil {
		pr.CloseWithError(err)
		return Backup{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}
	return Backup{ID: id, Path: path, Taken: at, Reason: reason, Instance: src.Instance, Size: info.Size(), Worlds: len(worlds), Names: worlds, Minecraft: src.Minecraft, Loader: src.Loader, LoaderVersion: src.LoaderVersion, seq: seq}, nil
}

// nextSeq is the collision counter for a backup taken at at: 1 when none in dir shares its second,
// else one past the highest there, the first of a second counting as 1.
func nextSeq(dir string, at time.Time) (int, error) {
	backups, err := Backups(dir)
	if err != nil {
		return 0, err
	}
	stamp := at.Format(timeLayout)
	seq := 1
	for _, b := range backups {
		if strings.HasPrefix(b.ID, stamp) {
			seq = max(seq, max(b.seq, 1)+1)
		}
	}
	return seq, nil
}

func writeZip(w io.Writer, dir string, worlds []string, meta string, each func(world string, open bool)) error {
	zw := zip.NewWriter(w)
	for _, world := range worlds {
		if each != nil {
			open, _ := InUse(filepath.Join(dir, world))
			each(world, open)
		}
		err := filepath.WalkDir(filepath.Join(dir, world), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && !d.Type().IsRegular() {
				return nil
			}
			// A running game's lock on Windows refuses reads of the file, and the game writes a
			// fresh one whenever it opens the world.
			if path == filepath.Join(dir, world, "session.lock") {
				return nil
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			h, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			h.Name = filepath.ToSlash(rel)
			if d.IsDir() {
				h.Name += "/"
				_, err := zw.CreateHeader(h)
				return err
			}
			h.Method = zip.Deflate
			fw, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(fw, f)
			return err
		})
		if err != nil {
			return err
		}
	}
	if err := zw.SetComment(meta); err != nil {
		return err
	}
	return zw.Close()
}

const (
	endLen  = 22
	maxTail = endLen + math.MaxUint16
)

var endSignature = []byte("PK\x05\x06")

// tailComment reads a zip's comment from its last bytes alone, since archive/zip reads the whole
// central directory first. The end record is the first signature whose comment length reaches
// exactly to the end of the file: a later one can only be inside the comment.
func tailComment(r io.ReaderAt, size int64) (string, bool) {
	n := min(size, maxTail)
	buf := make([]byte, n)
	if _, err := r.ReadAt(buf, size-n); err != nil && !errors.Is(err, io.EOF) {
		return "", false
	}
	for i := 0; ; i++ {
		j := bytes.Index(buf[i:], endSignature)
		if j < 0 {
			return "", false
		}
		i += j
		if i+endLen <= len(buf) && i+endLen+int(binary.LittleEndian.Uint16(buf[i+20:])) == len(buf) {
			return string(buf[i+endLen:]), true
		}
	}
}

// readBackup reads the backup named id from its zip. A comment shulker wrote is the whole record;
// without one, everything comes from the filename and the folders at the zip's root. A format this
// build doesn't know counts as no comment, so a newer shulker's backup still lists by its name. A
// zip with neither is not one of shulker's.
func readBackup(r io.ReaderAt, size int64, id string) (Backup, bool) {
	b := Backup{ID: id, Size: size}
	named := parseName(&b)
	raw, _ := tailComment(r, size)
	var meta comment
	if json.Unmarshal([]byte(raw), &meta) == nil && meta.Format == commentFormat {
		if taken, err := time.Parse(time.RFC3339, meta.Taken); err == nil {
			b.Taken, b.Reason, b.Instance = taken.Local(), meta.Reason, meta.Instance
			b.Worlds, b.Names = meta.WorldCount, meta.Worlds
			b.Minecraft, b.Loader, b.LoaderVersion = meta.Minecraft, meta.Loader, meta.LoaderVersion
			return b, true
		}
	}
	if !named {
		return Backup{}, false
	}
	b.Worlds = countRoots(r, size)
	return b, true
}

// parseName reads b's time, collision counter, instance and reason from its ID, named
// <time>[-<n>][-<instance>]-<reason>, and reports whether it is named that way.
func parseName(b *Backup) bool {
	if len(b.ID) < len(timeLayout) {
		return false
	}
	taken, err := time.ParseInLocation(timeLayout, b.ID[:len(timeLayout)], time.Local)
	if err != nil {
		return false
	}
	b.Taken = taken
	segs := strings.Split(strings.TrimPrefix(b.ID[len(timeLayout):], "-"), "-")
	if len(segs) > 1 {
		if n, err := strconv.Atoi(segs[0]); err == nil {
			b.seq, segs = n, segs[1:]
		}
	}
	b.Reason = segs[len(segs)-1]
	b.Instance = strings.Join(segs[:len(segs)-1], "-")
	return true
}

// countRoots counts the folders at a zip's root. A zip that won't open holds none.
func countRoots(r io.ReaderAt, size int64) int {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return 0
	}
	roots := map[string]bool{}
	for _, f := range zr.File {
		if root, _, ok := strings.Cut(f.Name, "/"); ok && root != "" {
			roots[root] = true
		}
	}
	return len(roots)
}
