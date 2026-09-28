// Package manual is what shulker knows about files a provider won't serve: which files a
// missing-files error asks for, and whether they have been dropped in by hand yet.
package manual

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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

// Status is where one file of a wait stands.
type Status struct {
	Found bool
}

// Wait looks for files dropped into a downloads folder, remembering each file's hashes by its size
// and modification time, so that checking every few seconds hashes only what changed.
type Wait struct {
	downloads string
	files     []File
	status    []Status
	seen      map[string]seen
}

type hashes struct{ sha1, sha512 string }

type seen struct {
	size    int64
	modTime time.Time
	hashes  hashes
}

func NewWait(downloads string, list []File) *Wait {
	return &Wait{downloads: downloads, files: list, status: make([]Status, len(list)), seen: map[string]seen{}}
}

// Check looks in the downloads folder again and says where each file stands. A file once found
// stays found.
func (w *Wait) Check() ([]Status, error) {
	entries, err := os.ReadDir(w.downloads)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") || !slices.Contains(Kinds, strings.ToLower(filepath.Ext(e.Name()))) {
			continue
		}
		h, err := w.hash(filepath.Join(w.downloads, e.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for i, f := range w.files {
			if f.matches(h) {
				w.status[i].Found = true
			}
		}
	}
	return slices.Clone(w.status), nil
}

// Done reports whether every file has been found.
func (w *Wait) Done() bool {
	return !slices.ContainsFunc(w.status, func(s Status) bool { return !s.Found })
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
