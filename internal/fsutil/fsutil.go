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
	return writeFrom(path, r, 0o644, nil)
}

// WriteFromChecked is WriteFrom that has check look at the written temp file before it replaces
// path; a check that fails leaves path as it was and no temp file behind.
func WriteFromChecked(path string, r io.Reader, check func(tmp string) error) error {
	return writeFrom(path, r, 0o644, check)
}

// writeFrom is WriteFrom with the mode a new file gets and an optional check.
func writeFrom(path string, r io.Reader, mode fs.FileMode, check func(tmp string) error) error {
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
	if check != nil {
		if err := check(tmp.Name()); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), path)
}

// Replace writes data to path after moving whatever is there aside. The new
// file keeps the old one's mode.
func Replace(path string, data []byte) (kept string, err error) {
	if path, err = follow(path); err != nil {
		return "", err
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	kept, err = MoveAside(path)
	if err != nil {
		return "", err
	}
	return kept, writeFrom(path, bytes.NewReader(data), mode, nil)
}

// MoveAside renames the file at path to <name>.replaced, clobbering only an
// older replaced file. kept is where it went, empty when there was none. A
// symlink at path is followed, so kept sits beside the link's target, not the
// link.
func MoveAside(path string) (kept string, err error) {
	if path, err = follow(path); err != nil {
		return "", err
	}
	kept = path + ".replaced"
	if err := os.Rename(path, kept); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return kept, nil
}

// follow is the target of a symlink at path, or path itself.
func follow(path string) (string, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return filepath.EvalSymlinks(path)
	}
	return path, nil
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

// tailChunk is how much of a file ReadTail takes at a time from its end.
const tailChunk = 64 << 10

// ReadTail is the file from its end back to where its last limit lines begin, a whole line further
// so those lines are whole, and leaves f at its end for following. With no limit it is the whole
// file from where f is.
func ReadTail(f *os.File, limit int) (string, error) {
	if limit == 0 {
		data, err := io.ReadAll(f)
		return string(data), err
	}
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}
	var data []byte
	for start := end; start > 0 && bytes.Count(data, []byte{'\n'}) <= limit; {
		next := max(0, start-tailChunk)
		chunk := make([]byte, start-next)
		if _, err := f.ReadAt(chunk, next); err != nil {
			return "", err
		}
		data = append(chunk, data...)
		start = next
	}
	return string(data), nil
}
