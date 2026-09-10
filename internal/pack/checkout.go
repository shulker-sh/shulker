package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/andrewmast/shulker/internal/fetch"
	"github.com/andrewmast/shulker/internal/lock"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
)

type Checkout struct {
	Source string `json:"source"`
	Kind   Kind   `json:"kind"`
	Dir    string `json:"dir"`
	Commit string `json:"commit,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
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
		if err != nil {
			return nil, err
		}
		if c.Commit, err = s.revParse(ctx, mirror, ref); err != nil {
			return nil, out.Errorf("source-ref", "ref %q not found in %s: %v", refOrHead(ref), source, err)
		}
		c.Dir, err = s.export(ctx, projectOrigin, mirror, c.Commit)
		return c, err
	default:
		return s.checkoutURL(ctx, c)
	}
}

func (s *Store) checkoutURL(ctx context.Context, c *Checkout) (*Checkout, error) {
	s.log("fetching project")
	manifestData, err := s.download(ctx, c.Source)
	if errors.Is(err, fetch.ErrNotFound) {
		return nil, out.Errorf("source-fetch", "no %s at %s", manifest.FileName, c.Source)
	}
	if err != nil {
		return nil, out.Errorf("source-fetch", "%v", err)
	}
	lockURL := c.Source[:strings.LastIndex(c.Source, "/")+1] + lock.FileName
	lockData, err := s.download(ctx, lockURL)
	if errors.Is(err, fetch.ErrNotFound) {
		return nil, out.Errorf("source-lock", "no %s beside %s; the project must be locked before it can be synced", lock.FileName, c.Source)
	}
	if err != nil {
		return nil, out.Errorf("source-fetch", "%v", err)
	}
	h := sha256.New()
	h.Write(manifestData)
	h.Write(lockData)
	c.Sha256 = hex.EncodeToString(h.Sum(nil))
	c.Dir = filepath.Join(s.CacheDir, "projects", "url", c.Sha256)
	if _, err := os.Stat(filepath.Join(c.Dir, lock.FileName)); err == nil {
		return c, nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(c.Dir, manifest.FileName), manifestData, 0o644); err != nil {
		return nil, err
	}
	return c, os.WriteFile(filepath.Join(c.Dir, lock.FileName), lockData, 0o644)
}

func (s *Store) download(ctx context.Context, url string) ([]byte, error) {
	var buf strings.Builder
	if _, err := s.Fetch.Download(ctx, url, &buf); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}
