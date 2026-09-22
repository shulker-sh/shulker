// Package zipfile writes the zips shulker hands to others, byte for byte the same on every machine and toolchain.
package zipfile

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/flate"
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

// Folder zips the regular files under root, leaving out what editors and file managers leave behind:
// any name starting with a dot, Thumbs.db, desktop.ini, and *~ and *.swp.
func Folder(root string) ([]byte, error) {
	// WalkDir doesn't follow a symlinked root, which would zip nothing.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	entries := map[string][]byte{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && isJunk(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return Build(entries, "")
}

func isJunk(name string) bool {
	switch {
	case strings.HasPrefix(name, "."), name == "Thumbs.db", name == "desktop.ini":
		return true
	}
	return strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".swp")
}
