// Package zipfile writes the zips shulker hands to others, byte for byte the same on every machine and toolchain.
package zipfile

import (
	"archive/zip"
	"bytes"
	"io"
	"sort"
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
