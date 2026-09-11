// Package fsutil writes files atomically: a reader, or a run cut short,
// sees either the old file or the new one, never a partial write.
package fsutil

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Write replaces path with data. A symlink at path is followed and its
// target replaced. A new file gets mode 0644; an existing one keeps its mode
// and must be writable.
func Write(path string, data []byte) error {
	return WriteFrom(path, bytes.NewReader(data))
}

func WriteFrom(path string, r io.Reader) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		// A rename needs only a writable directory, so check the file itself
		// to refuse a read-only file the way os.WriteFile would.
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		f.Close()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// MarshalJSON is the encoding of every JSON file shulker writes.
func MarshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WriteJSON(path string, v any) error {
	data, err := MarshalJSON(v)
	if err != nil {
		return err
	}
	return Write(path, data)
}
