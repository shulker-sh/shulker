// Package cache is shulker's download cache: files kept by their sha512, the pack and project
// checkouts remote sources build from, and the layout of both.
package cache

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
)

type Cache struct {
	Dir string
}

func Open() (*Cache, error) {
	if dir := os.Getenv("SHULKER_CACHE"); dir != "" {
		return &Cache{Dir: dir}, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return &Cache{Dir: filepath.Join(base, "shulker")}, nil
}

// Has reports whether the object sha is cached; a sha that isn't a sha512 names none.
func (c *Cache) Has(sha string) bool {
	if len(sha) != sha512.Size*2 {
		return false
	}
	_, err := os.Stat(c.Object(sha))
	return err == nil
}

func (c *Cache) Put(r io.Reader) (string, error) {
	return c.put(r, false)
}

// PutManual is Put for a file downloaded by hand, which no provider serves again: the object is
// marked manual.
func (c *Cache) PutManual(r io.Reader) (string, error) {
	return c.put(r, true)
}

func (c *Cache) put(r io.Reader, manual bool) (string, error) {
	tmp, err := c.TempFile("obj")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h, h1 := sha512.New(), sha1.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h, h1), r); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	return sha, c.commit(tmp.Name(), sha, hex.EncodeToString(h1.Sum(nil)), manual)
}

// commit lands the file at tmpPath as the object sha, and records it under its sha1, and as manual
// when it is. An object already there is recorded again all the same. The sha1 entry is written
// first, so a manual object is always found by it.
func (c *Cache) commit(tmpPath, sha, sum1 string, manual bool) error {
	dst := c.Object(sha)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if !c.Has(sha) {
		if err := os.Rename(tmpPath, dst); err != nil {
			return err
		}
	}
	entry := c.sha1Entry(sum1)
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		return err
	}
	if err := fsutil.Write(entry, []byte(sha)); err != nil || !manual {
		return err
	}
	return c.MarkManual(sha)
}

// MarkManual marks the cached object sha as downloaded by hand. Every object already has its sha1
// entry, so a manual object is always found by it.
func (c *Cache) MarkManual(sha string) error {
	marker := c.manualMarker(sha)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	return fsutil.Write(marker, nil)
}

// IsManual reports whether the object sha was downloaded by hand.
func (c *Cache) IsManual(sha string) bool {
	_, err := os.Stat(c.manualMarker(sha))
	return err == nil
}

// BySha1 is the sha512 of the cached object whose sha1 is sum1: how a file whose provider
// publishes only a sha1 is found without downloading it again.
func (c *Cache) BySha1(sum1 string) (string, bool) {
	if _, err := hex.DecodeString(sum1); err != nil || len(sum1) != sha1.Size*2 {
		return "", false
	}
	b, err := os.ReadFile(c.sha1Entry(sum1))
	if err != nil {
		return "", false
	}
	sha := string(b)
	return sha, c.Has(sha)
}

// dropIndexEntries removes each manual marker and sha1 entry whose object is gone. A marker is
// named for its object and a sha1 entry holds it.
func (c *Cache) dropIndexEntries() error {
	markers, err := entriesAt(filepath.Join(c.Dir, "index", "manual"), 1)
	if err != nil {
		return err
	}
	entries, err := entriesAt(filepath.Join(c.Dir, "index", "sha1"), 2)
	if err != nil {
		return err
	}
	for _, marker := range markers {
		if err := c.dropUnless(marker, filepath.Base(marker)); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		b, err := os.ReadFile(entry)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := c.dropUnless(entry, string(b)); err != nil {
			return err
		}
	}
	return nil
}

// dropUnless removes the index entry at path unless the object sha is cached.
func (c *Cache) dropUnless(path, sha string) error {
	if c.Has(sha) {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Ensure is the cached file with this sha512, downloaded from url first when it isn't cached. A
// download that hashes differently is refused.
func (c *Cache) Ensure(ctx context.Context, client *fetch.Client, url, sha string) (string, error) {
	if c.Has(sha) {
		return c.Object(sha), nil
	}
	_, err := c.fetch(ctx, client, url, func(got, _ string) error {
		if got == sha {
			return nil
		}
		e := out.Errorf("checksum-mismatch", "the download from %s doesn't match its sha512", url)
		e.Rows = []out.Detail{{Label: "want", Text: sha}, {Label: "got", Text: got}}
		return e
	})
	return c.Object(sha), err
}

// Fetch downloads url into the cache when its hash isn't known in advance, and returns the path.
func (c *Cache) Fetch(ctx context.Context, client *fetch.Client, url string) (string, error) {
	return c.fetch(ctx, client, url, nil)
}

// fetch downloads url into the cache, hashing it as it arrives, and returns its sha512. check sees
// both hashes before the file is committed, and a check that fails leaves nothing behind.
func (c *Cache) fetch(ctx context.Context, client *fetch.Client, url string, check func(sha, sum1 string) error) (string, error) {
	tmp, err := c.TempFile("dl")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h1 := sha1.New()
	sha, err := client.Download(ctx, url, io.MultiWriter(tmp, h1))
	tmp.Close()
	if err != nil {
		return "", err
	}
	sum1 := hex.EncodeToString(h1.Sum(nil))
	if check != nil {
		if err := check(sha, sum1); err != nil {
			return "", err
		}
	}
	return sha, c.commit(tmp.Name(), sha, sum1, false)
}

func (c *Cache) CopyTo(sha, dst string) error {
	src, err := os.Open(c.Object(sha))
	if err != nil {
		return err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFrom(dst, src)
}

// FetchChecked downloads a file the first time it is locked, checking the sha1 its source
// publishes, and returns its sha512. An empty sha1 checks nothing.
func (c *Cache) FetchChecked(ctx context.Context, client *fetch.Client, url, sha1 string) (string, error) {
	return c.fetch(ctx, client, url, func(_, got string) error {
		if sha1 == "" || got == sha1 {
			return nil
		}
		e := out.Errorf("checksum-mismatch", "the download from %s doesn't match the sha1 its source gives", url)
		e.Rows = []out.Detail{{Label: "want", Text: sha1}, {Label: "got", Text: got}}
		return e
	})
}
