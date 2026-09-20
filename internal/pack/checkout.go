package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

type Checkout struct {
	Source   string    `json:"source"`
	Kind     Kind      `json:"kind"`
	Dir      string    `json:"dir"`
	Commit   string    `json:"commit,omitempty"`
	Sha256   string    `json:"sha256,omitempty"`
	Offline  bool      `json:"offline,omitempty"`
	LastGood time.Time `json:"-"`
	Warning  string    `json:"-"`
	ref      string
}

var projectOrigin = origin{label: "project", code: "source-fetch"}

func (s *Store) Checkout(ctx context.Context, source, ref string) (*Checkout, error) {
	kind := Classify(source)
	if ref != "" && kind != Git {
		return nil, out.Errorf("source-ref", "--ref only applies to git sources")
	}
	c := &Checkout{Source: source, Kind: kind, ref: ref}
	var err error
	switch kind {
	case Local:
		c.Dir, err = filepath.Abs(source)
		return c, err
	case Git:
		mirror, err := s.ensureMirror(ctx, projectOrigin, source)
		if err != nil {
			if ctx.Err() == nil && fetch.IsNetwork(err) {
				return s.gitFallback(c, err)
			}
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
	s.log("fetching %s", c.Source)
	manifestData, err := s.download(ctx, c.Source)
	if err == nil {
		var lockData []byte
		if lockData, err = s.download(ctx, lockURL(c.Source)); errors.Is(err, fetch.ErrNotFound) {
			return nil, out.Errorf("source-lock", "no %s beside %s; the project must be locked before it can be synced", lock.FileName, c.Source)
		} else if err == nil {
			return c, s.storeURL(c, manifestData, lockData)
		}
	} else if errors.Is(err, fetch.ErrNotFound) {
		return nil, out.Errorf("source-fetch", "no %s at %s", manifest.FileName, c.Source)
	}
	if ctx.Err() == nil && fetch.IsNetwork(err) {
		return s.urlFallback(c, err)
	}
	return nil, out.Errorf("source-fetch", "%v", err)
}

// lockURL names the lock beside a manifest fetched from a raw URL, which holds for repo
// raw URLs and gist raw URLs alike.
func lockURL(manifestURL string) string {
	return manifestURL[:strings.LastIndex(manifestURL, "/")+1] + lock.FileName
}

func (s *Store) storeURL(c *Checkout, manifestData, lockData []byte) error {
	h := sha256.New()
	h.Write(manifestData)
	h.Write(lockData)
	c.Sha256 = hex.EncodeToString(h.Sum(nil))
	c.Dir = s.Cache.ProjectCheckout(c.Sha256)
	if _, err := os.Stat(filepath.Join(c.Dir, lock.FileName)); err == nil {
		return nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	if err := fsutil.Write(filepath.Join(c.Dir, manifest.FileName), manifestData); err != nil {
		return err
	}
	return fsutil.Write(filepath.Join(c.Dir, lock.FileName), lockData)
}

// lastGood records what a remote source looked like the last time a sync from it built
// successfully; it is what a sync falls back to when the source can't be reached.
type lastGood struct {
	Source string    `json:"source"`
	Ref    string    `json:"ref,omitempty"`
	Commit string    `json:"commit,omitempty"`
	Sha256 string    `json:"sha256,omitempty"`
	At     time.Time `json:"at"`
}

func (s *Store) readLastGood(source, ref string) (lastGood, bool) {
	var rec lastGood
	data, err := os.ReadFile(s.Cache.LastGood(source, ref))
	if err != nil || json.Unmarshal(data, &rec) != nil || rec.Source != source {
		return lastGood{}, false
	}
	return rec, true
}

// RecordGood marks c as the copy to fall back to. Call it only after a build from c succeeded,
// so a broken remote seen while online never becomes the offline fallback.
func (s *Store) RecordGood(c *Checkout) error {
	if c.Offline || (c.Kind != Git && c.Kind != URL) {
		return nil
	}
	rec := lastGood{Source: c.Source, Ref: c.ref, Commit: c.Commit, Sha256: c.Sha256, At: time.Now().UTC()}
	path := s.Cache.LastGood(c.Source, c.ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteJSON(path, rec)
}

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (s *Store) gitFallback(c *Checkout, cause error) (*Checkout, error) {
	c.Offline = true
	if fullCommit.MatchString(c.ref) {
		if dir := s.Cache.PackSource(c.ref); exists(dir) {
			c.Commit, c.Dir = c.ref, dir
			c.Warning = fmt.Sprintf("%s, using %s at %s, already downloaded", offlineReason(cause), c.Source, c.ref[:12])
			return c, nil
		}
		return nil, neverSynced(c, cause)
	}
	rec, ok := s.readLastGood(c.Source, c.ref)
	if !ok || rec.Commit == "" || !exists(s.Cache.PackSource(rec.Commit)) {
		return nil, neverSynced(c, cause)
	}
	c.Commit, c.Dir, c.LastGood = rec.Commit, s.Cache.PackSource(rec.Commit), rec.At
	c.Warning = fmt.Sprintf("%s, using %s at %s from the last successful sync %s", offlineReason(cause), c.Source, rec.Commit[:12], ago(rec.At))
	return c, nil
}

func (s *Store) urlFallback(c *Checkout, cause error) (*Checkout, error) {
	c.Offline = true
	rec, ok := s.readLastGood(c.Source, "")
	if !ok || rec.Sha256 == "" || !exists(filepath.Join(s.Cache.ProjectCheckout(rec.Sha256), lock.FileName)) {
		return nil, neverSynced(c, cause)
	}
	c.Sha256, c.Dir, c.LastGood = rec.Sha256, s.Cache.ProjectCheckout(rec.Sha256), rec.At
	c.Warning = fmt.Sprintf("%s, using %s from the last successful sync %s", offlineReason(cause), c.Source, ago(rec.At))
	return c, nil
}

func offlineReason(cause error) string {
	if errors.Is(cause, fetch.ErrOffline) {
		return "--offline"
	}
	return "offline"
}

func neverSynced(c *Checkout, cause error) error {
	what := c.Source
	if c.ref != "" {
		what = fmt.Sprintf("%s (ref %s)", c.Source, c.ref)
	}
	if errors.Is(cause, fetch.ErrOffline) {
		e := out.Errorf("source-offline", "--offline, and %s has never synced here", what)
		e.Help = "run it once without --offline to fetch a copy"
		return e
	}
	const noCopy = "it has never synced here, so there's no copy to fall back to"
	e := out.Errorf("source-offline", "couldn't reach %s\n%s", what, noCopy)
	if label, reason := unreachableReason(c, cause); reason != "" {
		e.Rows = append(e.Rows, out.Detail{Label: label, Text: reason})
	}
	e.Rows = append(e.Rows, out.Detail{Text: noCopy})
	e.Help = "check the address and that the server is running, then try again"
	return e
}

var (
	gitReasonPrefix  = regexp.MustCompile(`(?s)^.*? failed: (?:fatal: )?(?:unable to access '[^']*': )?`)
	httpReasonPrefix = regexp.MustCompile(`(?s)^.*?(?:Get|Head) "[^"]*": `)
)

// unreachableReason is git's or the HTTP client's own words for the failure, without the
// source URL they repeat.
func unreachableReason(c *Checkout, cause error) (string, string) {
	if c.Kind == Git {
		return "git", strings.TrimSpace(gitReasonPrefix.ReplaceAllString(cause.Error(), ""))
	}
	return "http", strings.TrimSpace(httpReasonPrefix.ReplaceAllString(cause.Error(), ""))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ago(t time.Time) string {
	d := time.Since(t)
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit + " ago"
		}
		return fmt.Sprintf("%d %ss ago", n, unit)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute")
	case d < 48*time.Hour:
		return plural(int(d/time.Hour), "hour")
	}
	return plural(int(d/(24*time.Hour)), "day")
}

func (s *Store) download(ctx context.Context, url string) ([]byte, error) {
	var buf strings.Builder
	if _, err := s.Fetch.Download(ctx, url, &buf); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}
