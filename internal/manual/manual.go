// Package manual is what shulker knows about files a provider won't serve: which files a
// missing-files error asks for, and whether they have been downloaded by hand yet.
package manual

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
)

// Kinds are the extensions a file a provider serves can have. Minecraft keeps its own files in a
// game dir's downloads/, which is an in-place instance's downloads folder: log.json, for one.
var Kinds = []string{".jar", ".zip"}

// File is one file to download by hand: the name it is published under, the page it comes from,
// and at least one of the hashes that prove a copy is it.
type File struct {
	Name   string
	Page   string
	Sha1   string
	Sha512 string
}

func (f File) matches(h hashes) bool {
	return f.Sha1 != "" && f.Sha1 == h.sha1 || f.Sha512 != "" && f.Sha512 == h.sha512
}

// files is how a missing-files error carries its files: wrapped, so neither the human error nor
// the JSON one shows them.
type files []File

func (files) Error() string { return "files to download by hand" }

// Attach records the files a missing-files error asks for, one per item it lists.
func Attach(e *out.Error, list []File) {
	e.Wrapped = files(list)
}

// Of is the files the missing-files error in err asks for, or nil when it carries none.
func Of(err error) []File {
	var f files
	if errors.As(err, &f) {
		return f
	}
	return nil
}

// Status is where one file of a wait stands: found, or not with a note on a copy that isn't it.
type Status struct {
	Found bool
	Note  string
}

// Wait looks for files dropped into a project's downloads folder, where any file of the right
// kind is hashed, and into watched folders, where only a file with the expected name, a browser's
// duplicate of it, or a file that arrived after the wait started is. A find in a watched folder
// lands in the downloads folder under its expected name: copied when it was already there when
// the wait started, moved when it arrived during it. Each file's hashes are remembered by its size
// and modification time, so checking every few seconds hashes only what changed.
type Wait struct {
	downloads string
	watch     []string
	files     []File
	status    []Status
	started   time.Time
	before    map[string]bool
	seen      map[string]seen
	kinds     []string
}

type hashes struct{ sha1, sha512 string }

type seen struct {
	size    int64
	modTime time.Time
	hashes  hashes
}

// candidate is a file in a watched folder that could be one the wait wants.
type candidate struct {
	path    string
	name    string
	modTime time.Time
	arrived bool
}

func NewWait(downloads string, watch []string, list []File) *Wait {
	w := &Wait{downloads: downloads, files: list, status: make([]Status, len(list)), started: time.Now(), before: map[string]bool{}, seen: map[string]seen{}, kinds: slices.Clone(Kinds)}
	for _, f := range list {
		if ext := strings.ToLower(filepath.Ext(f.Name)); ext != "" && !slices.Contains(w.kinds, ext) {
			w.kinds = append(w.kinds, ext)
		}
	}
	for _, dir := range watch {
		if filepath.Clean(dir) == filepath.Clean(downloads) || slices.Contains(w.watch, dir) {
			continue
		}
		w.watch = append(w.watch, dir)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			w.before[filepath.Join(dir, e.Name())] = true
		}
	}
	return w
}

// Check looks in the downloads folder and every watched folder again and says where each file
// stands. A file once found stays found.
func (w *Wait) Check() ([]Status, error) {
	entries, err := w.read(w.downloads)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		h, err := w.hash(filepath.Join(w.downloads, e.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for i, f := range w.files {
			if f.matches(h) {
				w.status[i] = Status{Found: true}
			}
		}
	}
	for _, dir := range w.watch {
		if err := w.checkWatched(dir); err != nil {
			return nil, err
		}
	}
	return slices.Clone(w.status), nil
}

// checkWatched looks through one watched folder. A folder or file it can't read, such as a
// Downloads folder the OS keeps from the terminal, is passed over rather than ending the wait.
func (w *Wait) checkWatched(dir string) error {
	entries, err := w.read(dir)
	if err != nil {
		return nil
	}
	var candidates []candidate
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		arrived := !w.before[path] || info.ModTime().After(w.started)
		candidates = append(candidates, candidate{path: path, name: e.Name(), modTime: info.ModTime(), arrived: arrived})
	}
	slices.SortFunc(candidates, func(a, b candidate) int { return b.modTime.Compare(a.modTime) })
	for i, f := range w.files {
		if w.status[i].Found {
			continue
		}
		named := false
		for _, c := range candidates {
			isNamed := f.namedBy(c.name)
			if !isNamed && !c.arrived {
				continue
			}
			h, err := w.hash(c.path)
			if err != nil {
				continue
			}
			if f.matches(h) {
				if err := w.take(c, f); err != nil {
					return err
				}
				w.status[i] = Status{Found: true}
				break
			}
			if isNamed && !named {
				named = true
				w.status[i].Note = fmt.Sprintf("%s in %s isn't the expected file", c.name, dir)
			}
		}
	}
	return nil
}

// namedBy reports whether name is the file's own name or a browser's duplicate of it, like
// "name (1).jar".
func (f File) namedBy(name string) bool {
	if name == f.Name {
		return true
	}
	ext := filepath.Ext(f.Name)
	rest, ok := strings.CutPrefix(name, strings.TrimSuffix(f.Name, ext)+" (")
	if !ok {
		return false
	}
	n, ok := strings.CutSuffix(rest, ")"+ext)
	return ok && n != "" && strings.Trim(n, "0123456789") == ""
}

// take lands c in the downloads folder under f's name, moving it when it arrived during the wait
// and copying it otherwise.
func (w *Wait) take(c candidate, f File) error {
	if err := os.MkdirAll(w.downloads, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(w.downloads, f.Name)
	if c.arrived && os.Rename(c.path, dest) == nil {
		return nil
	}
	src, err := os.Open(c.path)
	if err != nil {
		return err
	}
	err = fsutil.WriteFrom(dest, src)
	src.Close()
	if err != nil || !c.arrived {
		return err
	}
	return os.Remove(c.path)
}

// read lists the files in dir a provider could have served, or with the extension of a file the
// wait wants, such as a modpack's .mrpack; none when dir doesn't exist.
func (w *Wait) read(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(entries, func(e os.DirEntry) bool {
		return !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") || !slices.Contains(w.kinds, strings.ToLower(filepath.Ext(e.Name())))
	}), nil
}

func (w *Wait) hash(path string) (hashes, error) {
	st, err := os.Stat(path)
	if err != nil {
		return hashes{}, err
	}
	if s, ok := w.seen[path]; ok && s.size == st.Size() && s.modTime.Equal(st.ModTime()) {
		return s.hashes, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return hashes{}, err
	}
	defer f.Close()
	h1, h512 := sha1.New(), sha512.New()
	if _, err := io.Copy(io.MultiWriter(h1, h512), f); err != nil {
		return hashes{}, err
	}
	h := hashes{hex.EncodeToString(h1.Sum(nil)), hex.EncodeToString(h512.Sum(nil))}
	w.seen[path] = seen{size: st.Size(), modTime: st.ModTime(), hashes: h}
	return h, nil
}
