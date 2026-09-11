package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

type Checkout struct {
	Source  string `json:"source"`
	Kind    Kind   `json:"kind"`
	Dir     string `json:"dir"`
	Commit  string `json:"commit,omitempty"`
	Sha256  string `json:"sha256,omitempty"`
	Warning string `json:"-"`
}

var projectOrigin = origin{label: "project", code: "source-fetch"}

func (s *Store) Checkout(ctx context.Context, source, ref string) (*Checkout, error) {
	kind := Classify(source)
	if ref != "" && kind != Git {
		return nil, out.Errorf("source-ref", "--ref only applies to git sources")
	}
	c := &Checkout{Source: source, Kind: kind}
	var err error
	switch kind {
	case Local:
		c.Dir, err = filepath.Abs(source)
		return c, err
	case Git:
		mirror, err := s.ensureMirror(ctx, projectOrigin, source)
		cached := false
		if err != nil {
			if mirror = s.cachedMirror(source); mirror == "" || !unreachable(ctx, err) {
				return nil, err
			}
			cached = true
		}
		if c.Commit, err = s.revParse(ctx, mirror, ref); err != nil {
			if cached {
				return nil, out.Errorf("source-ref", "couldn't reach %s, and ref %q isn't in the cached copy", source, refOrHead(ref))
			}
			return nil, out.Errorf("source-ref", "ref %q not found in %s: %v", refOrHead(ref), source, err)
		}
		if cached {
			c.Warning = fmt.Sprintf("couldn't reach %s; using the cached copy from commit %s", source, c.Commit[:12])
		}
		c.Dir, err = s.export(ctx, projectOrigin, mirror, c.Commit)
		return c, err
	default:
		return s.checkoutURL(ctx, c)
	}
}

func (s *Store) cachedMirror(source string) string {
	dir := s.mirrorDir(source)
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

func unreachable(ctx context.Context, err error) bool {
	return ctx.Err() == nil && out.CodeOf(err) != "git-missing"
}

func (s *Store) checkoutURL(ctx context.Context, c *Checkout) (*Checkout, error) {
	s.log("fetching project")
	manifestData, err := s.download(ctx, c.Source)
	if errors.Is(err, fetch.ErrNotFound) {
		return nil, out.Errorf("source-fetch", "no %s at %s", manifest.FileName, c.Source)
	}
	if err != nil {
		return s.cachedURL(ctx, c, out.Errorf("source-fetch", "%v", err))
	}
	lockURL := c.Source[:strings.LastIndex(c.Source, "/")+1] + lock.FileName
	lockData, err := s.download(ctx, lockURL)
	if errors.Is(err, fetch.ErrNotFound) {
		return nil, out.Errorf("source-lock", "no %s beside %s; the project must be locked before it can be synced", lock.FileName, c.Source)
	}
	if err != nil {
		return s.cachedURL(ctx, c, out.Errorf("source-fetch", "%v", err))
	}
	h := sha256.New()
	h.Write(manifestData)
	h.Write(lockData)
	c.Sha256 = hex.EncodeToString(h.Sum(nil))
	c.Dir = filepath.Join(s.CacheDir, "projects", "url", c.Sha256)
	if _, err := os.Stat(filepath.Join(c.Dir, lock.FileName)); err != nil {
		if err := os.MkdirAll(c.Dir, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(c.Dir, manifest.FileName), manifestData, 0o644); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(c.Dir, lock.FileName), lockData, 0o644); err != nil {
			return nil, err
		}
	}
	return c, s.rememberURL(c)
}

func (s *Store) lastURLPath(source string) string {
	sum := sha256.Sum256([]byte(source))
	return filepath.Join(s.CacheDir, "projects", "url-last", hex.EncodeToString(sum[:]))
}

func (s *Store) rememberURL(c *Checkout) error {
	path := s.lastURLPath(c.Source)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(c.Sha256+"\n"), 0o644)
}

func (s *Store) cachedURL(ctx context.Context, c *Checkout, fetchErr error) (*Checkout, error) {
	if ctx.Err() != nil {
		return nil, fetchErr
	}
	path := s.lastURLPath(c.Source)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fetchErr
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fetchErr
	}
	c.Sha256 = strings.TrimSpace(string(data))
	c.Dir = filepath.Join(s.CacheDir, "projects", "url", c.Sha256)
	if _, err := os.Stat(filepath.Join(c.Dir, lock.FileName)); err != nil {
		return nil, fetchErr
	}
	c.Warning = fmt.Sprintf("couldn't reach %s; using the cached copy from %s", c.Source, info.ModTime().Format("2006-01-02 15:04"))
	return c, nil
}

func (s *Store) download(ctx context.Context, url string) ([]byte, error) {
	var buf strings.Builder
	if _, err := s.Fetch.Download(ctx, url, &buf); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}
