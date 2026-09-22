// Package zipfile writes the zips shulker hands to others, byte for byte the same on every machine and toolchain.
package zipfile

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/flate"

	"shulker.sh/shulker/internal/out"
)

// Modified is the time every entry carries.
var Modified = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// Build zips entries, keyed by slash path, with first (when set) at the front and the rest in path order.
//
// Deflate comes from klauspost's flate, pinned in go.mod: the standard library's output is outside the Go 1 promise
// and changed in Go 1.27, which would move a locked sha512 on the same sources.
func Build(entries map[string][]byte, first string) ([]byte, error) {
	names := make([]string, 0, len(entries))
	for n := range entries {
		if n != first {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if _, ok := entries[first]; ok {
		names = append([]string{first}, names...)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Deflate, Modified: Modified})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(entries[n]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Folder zips the files under root, leaving out what editors and file managers leave behind: any
// name starting with a dot, Thumbs.db, desktop.ini, and *~ and *.swp. A symlink zips as what it
// points at.
func Folder(root string) ([]byte, error) {
	files, err := FolderFiles(root)
	if err != nil {
		return nil, err
	}
	entries := make(map[string][]byte, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		entries[f.Name] = data
	}
	return Build(entries, "")
}

// A FolderFile is a file Folder zips: Name is its slash path in the zip, Path where its bytes are
// read from, and Link the symlink under the folder it was reached through, as a slash path, or ""
// for a file reached without one.
type FolderFile struct {
	Name, Path, Link string
}

// FolderFiles is every file Folder zips from the folder at root, following symlinks to files and
// folders alike. A symlink to nothing, or to a folder it lies in, is refused.
func FolderFiles(root string) ([]FolderFile, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	w := folderWalk{folder: filepath.Base(root)}
	if err := w.walk(root, "", "", []string{root}); err != nil {
		return nil, err
	}
	return w.files, nil
}

type folderWalk struct {
	folder string
	files  []FolderFile
}

// walk adds the files under dir, a real path, whose slash path in the zip is name. open holds the
// real paths of the folders being walked, which a symlinked folder may not lead back into.
func (w *folderWalk) walk(dir, name, link string, open []string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, d := range entries {
		if Excluded(d.Name()) {
			continue
		}
		from, rel, via := filepath.Join(dir, d.Name()), path.Join(name, d.Name()), link
		isLink := d.Type()&fs.ModeSymlink != 0
		if isLink {
			target, err := filepath.EvalSymlinks(from)
			if err != nil {
				return out.Errorf("file-not-found", "%s: %s is a symlink to nothing", w.folder, rel)
			}
			from = target
			if via == "" {
				via = rel
			}
		}
		st, err := os.Stat(from)
		if err != nil {
			return err
		}
		switch {
		case st.IsDir():
			if isLink && slices.ContainsFunc(open, func(o string) bool { return within(from, o) }) {
				return out.Errorf("usage", "%s: %s is a symlink to a folder it lies in, so it can't be followed", w.folder, rel)
			}
			if err := w.walk(from, rel, via, append(open, from)); err != nil {
				return err
			}
		case st.Mode().IsRegular():
			w.files = append(w.files, FolderFile{Name: rel, Path: from, Link: via})
		}
	}
	return nil
}

func within(dir, sub string) bool {
	rel, err := filepath.Rel(dir, sub)
	return err == nil && filepath.IsLocal(rel)
}

// Excluded reports whether Folder leaves a file or folder of this name out.
func Excluded(name string) bool {
	switch {
	case strings.HasPrefix(name, "."), name == "Thumbs.db", name == "desktop.ini":
		return true
	}
	return strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".swp")
}
