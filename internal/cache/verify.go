package cache

import (
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Object is one file in objects/, by the sha512 it is stored under.
type Object struct {
	Sha512 string `json:"sha512"`
	Size   int64  `json:"size"`
}

// Rehash hashes every object again and returns those whose bytes no longer match the sha512 they
// are stored under, and how many it hashed.
func (c *Cache) Rehash() (changed []Object, hashed int, err error) {
	paths, err := entriesAt(filepath.Join(c.Dir, "objects"), 2)
	if err != nil {
		return nil, 0, err
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if !isSha512(name) {
			continue
		}
		sum, size, err := hashObject(path)
		if err != nil {
			return nil, 0, err
		}
		hashed++
		if sum != name {
			changed = append(changed, Object{Sha512: name, Size: size})
		}
	}
	return changed, hashed, nil
}

func hashObject(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha512.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func isSha512(name string) bool {
	if len(name) != 2*sha512.Size {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}

// Unused are the objects no root references, leaving out the manual downloads a prune keeps.
func (c *Cache) Unused(roots []Root) ([]Object, error) {
	keep := c.keep(roots)
	manual, err := c.manualObjects()
	if err != nil {
		return nil, err
	}
	paths, err := entriesAt(filepath.Join(c.Dir, "objects"), 2)
	if err != nil {
		return nil, err
	}
	var unused []Object
	for _, path := range paths {
		if keep[path] || manual[path] {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		unused = append(unused, Object{Sha512: filepath.Base(path), Size: info.Size()})
	}
	return unused, nil
}

// Drop removes the object stored under sha, so the next build that needs it downloads it again.
func (c *Cache) Drop(sha string) error {
	if err := os.Remove(c.Object(sha)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
