// Package cache is shulker's download cache: files kept by their sha512, the pack and project
// checkouts remote sources build from, and the layout of both.
package cache

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
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

func (c *Cache) Has(sha string) bool {
	if sha == "" {
		return false
	}
	_, err := os.Stat(c.Object(sha))
	return err == nil
}

func (c *Cache) Put(r io.Reader) (string, error) {
	tmp, err := c.TempFile("obj")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha512.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), r); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	return sha, c.commit(tmp.Name(), sha)
}

func (c *Cache) commit(tmpPath, sha string) error {
	dst := c.Object(sha)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if c.Has(sha) {
		return nil
	}
	return os.Rename(tmpPath, dst)
}

// Ensure is the cached file with this sha512, downloaded from url first when it isn't cached. A
// download that hashes differently is refused.
func (c *Cache) Ensure(ctx context.Context, client *fetch.Client, url, sha string) (string, error) {
	if c.Has(sha) {
		return c.Object(sha), nil
	}
	tmp, err := c.TempFile("dl")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	got, err := client.Download(ctx, url, tmp)
	tmp.Close()
	if err != nil {
		return "", err
	}
	if got != sha {
		e := out.Errorf("checksum-mismatch", "the download from %s doesn't match its sha512", url)
		e.Rows = []out.Detail{{Label: "want", Text: sha}, {Label: "got", Text: got}}
		return "", e
	}
	return c.Object(sha), c.commit(tmp.Name(), sha)
}

// Fetch downloads url into the cache when its hash isn't known in advance, and returns the path.
func (c *Cache) Fetch(ctx context.Context, client *fetch.Client, url string) (string, error) {
	tmp, err := c.TempFile("dl")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	sha, err := client.Download(ctx, url, tmp)
	tmp.Close()
	if err != nil {
		return "", err
	}
	return sha, c.commit(tmp.Name(), sha)
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
// publishes, and returns its sha512.
func (c *Cache) FetchChecked(ctx context.Context, client *fetch.Client, url, sha1 string) (string, error) {
	sha, err := c.Fetch(ctx, client, url)
	if err != nil || sha1 == "" {
		return sha, err
	}
	got, err := fsutil.SHA1(c.Object(sha))
	if err != nil {
		return "", err
	}
	if got != sha1 {
		e := out.Errorf("checksum-mismatch", "the download from %s doesn't match the sha1 its metadata gives", url)
		e.Rows = []out.Detail{{Label: "want", Text: sha1}, {Label: "got", Text: got}}
		return "", e
	}
	return sha, nil
}
