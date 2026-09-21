package saves

import (
	"archive/zip"
	"encoding/json"
	"io"
	"io/fs"
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

// Source is where a backup's worlds come from. Only narrows it to one world, a server's
// level-name; the rest is what the zip comment records.
type Source struct {
	Dir       string
	Only      string
	Instance  string
	Minecraft string
	Loader    string
}

// Home is the folder backups go in. A shared one is a save group's, where the filename also names
// the instance that took the backup.
type Home struct {
	Dir    string
	Shared bool
}

// Taken is a backup just written, and the worlds it holds.
type Taken struct {
	Backup
	Names []string `json:"names"`
}

// comment is the zip comment: display only, since the filename is what retention and ordering read.
type comment struct {
	Format    int      `json:"format"`
	Taken     string   `json:"taken"`
	Reason    string   `json:"reason"`
	Instance  string   `json:"instance,omitempty"`
	Worlds    []string `json:"worlds"`
	Minecraft string   `json:"minecraft,omitempty"`
	Loader    string   `json:"loader,omitempty"`
}

// Take zips src's worlds into a new backup in home, calling each before it zips a world. With no
// worlds to zip it writes nothing and returns a zero Taken rather than failing, so a caller backing
// up on the way past something else can carry on.
func Take(src Source, home Home, reason string, each func(world string)) (Taken, error) {
	worlds, err := Worlds(src.Dir)
	if err != nil {
		return Taken{}, err
	}
	if src.Only != "" {
		worlds = slices.DeleteFunc(worlds, func(w string) bool { return w != src.Only })
	}
	if len(worlds) == 0 {
		return Taken{}, nil
	}
	if err := os.MkdirAll(home.Dir, 0o755); err != nil {
		return Taken{}, err
	}
	at := now()
	seq, err := nextSeq(home.Dir, at)
	if err != nil {
		return Taken{}, err
	}
	id := at.Format(timeLayout)
	if seq > 1 {
		id += "-" + strconv.Itoa(seq)
	}
	if home.Shared && src.Instance != "" {
		id += "-" + src.Instance
	}
	id += "-" + reason
	meta, err := json.Marshal(comment{
		Format:    commentFormat,
		Taken:     at.UTC().Format(time.RFC3339),
		Reason:    reason,
		Instance:  src.Instance,
		Worlds:    worlds,
		Minecraft: src.Minecraft,
		Loader:    src.Loader,
	})
	if err != nil {
		return Taken{}, err
	}
	path := filepath.Join(home.Dir, id+".zip")
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(writeZip(pw, src.Dir, worlds, string(meta), each)) }()
	if err := fsutil.WriteFrom(path, pr); err != nil {
		pr.CloseWithError(err)
		return Taken{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Taken{}, err
	}
	b := Backup{ID: id, Path: path, Taken: at, Reason: reason, Size: info.Size(), Worlds: len(worlds), Minecraft: src.Minecraft, Loader: src.Loader, seq: seq}
	return Taken{Backup: b, Names: worlds}, nil
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
		if b.ID[:len(timeLayout)] == stamp {
			seq = max(seq, max(b.seq, 1)+1)
		}
	}
	return seq, nil
}

func writeZip(w io.Writer, dir string, worlds []string, meta string, each func(world string)) error {
	zw := zip.NewWriter(w)
	for _, world := range worlds {
		if each != nil {
			each(world)
		}
		err := filepath.WalkDir(filepath.Join(dir, world), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && !d.Type().IsRegular() {
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

// readZip counts the world folders at a zip's root and reads what its comment says it was taken
// from. A zip that won't open, or has no comment, says what it can.
func readZip(b *Backup) {
	r, err := zip.OpenReader(b.Path)
	if err != nil {
		return
	}
	defer r.Close()
	roots := map[string]bool{}
	for _, f := range r.File {
		if root, _, ok := strings.Cut(f.Name, "/"); ok && root != "" {
			roots[root] = true
		}
	}
	b.Worlds = len(roots)
	var meta comment
	if json.Unmarshal([]byte(r.Comment), &meta) == nil {
		b.Minecraft, b.Loader = meta.Minecraft, meta.Loader
	}
}
