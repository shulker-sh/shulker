// Package fsutil writes files atomically: a reader, or a run cut short,
// sees either the old file or the new one, never a partial write.
package fsutil

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
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
	return writeFrom(path, r, 0o644)
}

// writeFrom is WriteFrom with the mode a new file gets.
func writeFrom(path string, r io.Reader, mode fs.FileMode) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
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

// Replace writes data to path after renaming whatever is there to
// <name>.replaced, clobbering only an older replaced file. kept is where the
// old file went, empty when there was none. A symlink at path is followed, so
// kept sits beside the link's target, not the link. The new file keeps the old
// one's mode.
func Replace(path string, data []byte) (kept string, err error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		if path, err = filepath.EvalSymlinks(path); err != nil {
			return "", err
		}
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	kept = path + ".replaced"
	if err := os.Rename(path, kept); errors.Is(err, fs.ErrNotExist) {
		kept = ""
	} else if err != nil {
		return "", err
	}
	return kept, writeFrom(path, bytes.NewReader(data), mode)
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

// SHA1 is the hex sha1 of the file at path, the hash Mojang and CurseForge publish.
func SHA1(path string) (string, error) {
	return hashFile(path, sha1.New())
}

// SHA512 is the hex sha512 of the file at path, the key the cache stores it under.
func SHA512(path string) (string, error) {
	return hashFile(path, sha512.New())
}

func hashFile(path string, h hash.Hash) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
