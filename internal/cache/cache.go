package cache

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/fetch"
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

func (c *Cache) Path(sha string) string {
	return filepath.Join(c.Dir, "objects", sha[:2], sha)
}

func (c *Cache) Has(sha string) bool {
	_, err := os.Stat(c.Path(sha))
	return err == nil
}

func (c *Cache) Put(r io.Reader) (string, error) {
	if err := os.MkdirAll(filepath.Join(c.Dir, "tmp"), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Join(c.Dir, "tmp"), "obj-*")
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
	dst := c.Path(sha)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if c.Has(sha) {
		return nil
	}
	return os.Rename(tmpPath, dst)
}

func (c *Cache) Ensure(ctx context.Context, client *fetch.Client, url, sha string) (string, error) {
	if c.Has(sha) {
		return c.Path(sha), nil
	}
	if err := os.MkdirAll(filepath.Join(c.Dir, "tmp"), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Join(c.Dir, "tmp"), "dl-*")
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
		return "", fmt.Errorf("%s: sha512 mismatch (expected %s…, got %s…)", url, sha[:12], got[:12])
	}
	return c.Path(sha), c.commit(tmp.Name(), sha)
}

func (c *Cache) Fetch(ctx context.Context, client *fetch.Client, url string) (string, error) {
	if err := os.MkdirAll(filepath.Join(c.Dir, "tmp"), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Join(c.Dir, "tmp"), "dl-*")
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
	src, err := os.Open(c.Path(sha))
	if err != nil {
		return err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
