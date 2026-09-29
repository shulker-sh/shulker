package modpack

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
	"shulker.sh/shulker/internal/security"
)

// At is where in a git source a project is read: the ref to resolve, the remote HEAD when empty,
// and the folder of the repository holding its shulker.json, the root when empty.
type At struct {
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

// Checkout is a project source fetched for a sync, or the copy kept from its last good sync when
// the source can't be reached.
type Checkout struct {
	Source string `json:"source"`
	Kind   Kind   `json:"kind"`
	At
	Dir      string    `json:"dir"`
	Commit   string    `json:"commit,omitempty"`
	Sha256   string    `json:"sha256,omitempty"`
	Offline  bool      `json:"offline,omitempty"`
	LastGood time.Time `json:"-"`
	Warning  string    `json:"-"`
}

var projectOrigin = origin{label: "project", code: "source-fetch"}

// Checkout fetches a project from a directory, git repository or manifest URL.
func (s *Store) Checkout(ctx context.Context, source string, at At) (*Checkout, error) {
	kind := Classify(source)
	if at.Ref != "" && kind != Git {
		return nil, out.Errorf("source-ref", "--ref only applies to git sources")
	}
	if err := CheckPath(at.Path, kind); err != nil {
		return nil, err
	}
	c := &Checkout{Source: source, Kind: kind, At: at}
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
		if c.Commit, err = s.revParse(ctx, mirror, at.Ref); err != nil {
			return nil, refNotFound("source-ref", at.Ref, source, err)
		}
		export, err := s.export(ctx, projectOrigin, mirror, c.Commit)
		if err != nil {
			return nil, err
		}
		if c.Dir, err = subfolder(export, at.Path); err != nil {
			return nil, err
		}
		if at.Path != "" && !isOnDisk(filepath.Join(c.Dir, manifest.FileName)) {
			return nil, out.Errorf("source-path", "no %s in %s of %s at %s", manifest.FileName, at.Path, source, c.Commit[:12])
		}
		return c, nil
	default:
		if _, err := s.checkoutURL(ctx, c); err != nil {
			return nil, err
		}
		return c, s.checkRawURL(c)
	}
}

// checkRawURL fails a project fetched from a raw manifest URL that names a local file, which
// can't have come with it. A manifest that doesn't load is left for opening the project to report.
func (s *Store) checkRawURL(c *Checkout) error {
	m, err := manifest.Load(filepath.Join(c.Dir, manifest.FileName))
	if err != nil {
		return nil
	}
	if key, file, ok := localFile(m); ok {
		e := out.Errorf("source-incomplete", "requires.%s is the local file %s, and a manifest fetched from a raw URL carries no files", key, file)
		e.Help = "sync from the repository's git URL instead, with --path for a pack in a subfolder"
		return e
	}
	return nil
}

func (s *Store) checkoutURL(ctx context.Context, c *Checkout) (*Checkout, error) {
	s.log("fetching %s", c.Source)
	manifestData, err := s.download(ctx, c.Source)
	if errors.Is(err, fetch.ErrNotFound) {
		return nil, out.Errorf("source-fetch", "no %s at %s", manifest.FileName, c.Source)
	}
	if err != nil {
		return s.urlFailure(ctx, c, err)
	}
	lockData, err := s.download(ctx, lockURL(c.Source))
	if errors.Is(err, fetch.ErrNotFound) {
		e := out.Errorf("source-lock", "no %s beside %s", lock.FileName, c.Source)
		e.Help = "run `shulker lock` in the project and publish its lock beside the manifest"
		return nil, e
	}
	if err != nil {
		return s.urlFailure(ctx, c, err)
	}
	return c, s.storeURL(c, manifestData, lockData)
}

func (s *Store) urlFailure(ctx context.Context, c *Checkout, err error) (*Checkout, error) {
	if ctx.Err() == nil && fetch.IsNetwork(err) {
		return s.urlFallback(c, err)
	}
	e := out.Errorf("source-fetch", "couldn't fetch %s", c.Source)
	e.Rows = []out.Detail{{Label: "http", Text: httpReason(err)}}
	return nil, e
}

// CheckPath fails a path that isn't a folder inside a repository, or that is given for a source
// that isn't one.
func CheckPath(path string, kind Kind) error {
	if path == "" {
		return nil
	}
	if kind != Git {
		return out.Errorf("source-path", "--path only applies to git sources")
	}
	if !manifest.IsSubfolder(path) {
		e := out.Errorf("source-path", "--path %s is not a folder inside the repository", path)
		e.Help = "give a slash-separated path relative to the repository root, such as packs/survival"
		return e
	}
	return nil
}

// subfolder is the folder path names inside the checkout at root. A lock or a manifest from the
// source names path, so one that leaves root is refused rather than read.
func subfolder(root, path string) (string, error) {
	if path == "" {
		return root, nil
	}
	if !manifest.IsSubfolder(path) {
		return "", security.Refusal(security.Paths, out.Errorf("path-outside", "%s is outside the repository", path))
	}
	return filepath.Join(root, filepath.FromSlash(path)), nil
}

func refNotFound(code, ref, source string, err error) error {
	if out.CodeOf(err) == "git-missing" {
		return err
	}
	e := out.Errorf(code, "ref %q not found in %s", refOrHead(ref), source)
	e.Rows = []out.Detail{{Label: "git", Text: gitReason(err.Error())}}
	return e
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
	Path   string    `json:"path,omitempty"`
	Commit string    `json:"commit,omitempty"`
	Sha256 string    `json:"sha256,omitempty"`
	At     time.Time `json:"at"`
}

func (s *Store) readLastGood(source string, at At) (lastGood, bool) {
	var rec lastGood
	data, err := os.ReadFile(s.Cache.LastGood(source, at.Ref, at.Path))
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
	rec := lastGood{Source: c.Source, Ref: c.Ref, Path: c.Path, Commit: c.Commit, Sha256: c.Sha256, At: time.Now().UTC()}
	path := s.Cache.LastGood(c.Source, c.Ref, c.Path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteJSON(path, rec)
}

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (s *Store) gitFallback(c *Checkout, cause error) (*Checkout, error) {
	c.Offline = true
	if fullCommit.MatchString(c.Ref) {
		if dir := s.Cache.PackSource(c.Ref); isOnDisk(dir) {
			sub, err := subfolder(dir, c.Path)
			if err != nil {
				return nil, err
			}
			c.Commit, c.Dir = c.Ref, sub
			c.Warning = out.Sentence(fmt.Sprintf("%s, using %s at %s, already downloaded", offlineReason(cause), c.Source, c.Ref[:12]))
			return c, nil
		}
		return nil, neverSynced(c, cause)
	}
	rec, ok := s.readLastGood(c.Source, c.At)
	if !ok || rec.Commit == "" || !isOnDisk(s.Cache.PackSource(rec.Commit)) {
		return nil, neverSynced(c, cause)
	}
	sub, err := subfolder(s.Cache.PackSource(rec.Commit), c.Path)
	if err != nil {
		return nil, err
	}
	c.Commit, c.Dir, c.LastGood = rec.Commit, sub, rec.At
	c.Warning = out.Sentence(fmt.Sprintf("%s, using %s at %s from the last successful sync %s", offlineReason(cause), c.Source, rec.Commit[:12], out.Ago(rec.At)))
	return c, nil
}

func (s *Store) urlFallback(c *Checkout, cause error) (*Checkout, error) {
	c.Offline = true
	rec, ok := s.readLastGood(c.Source, At{})
	if !ok || rec.Sha256 == "" || !isOnDisk(filepath.Join(s.Cache.ProjectCheckout(rec.Sha256), lock.FileName)) {
		return nil, neverSynced(c, cause)
	}
	c.Sha256, c.Dir, c.LastGood = rec.Sha256, s.Cache.ProjectCheckout(rec.Sha256), rec.At
	c.Warning = out.Sentence(fmt.Sprintf("%s, using %s from the last successful sync %s", offlineReason(cause), c.Source, out.Ago(rec.At)))
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
	var at []string
	if c.Ref != "" {
		at = append(at, "ref "+c.Ref)
	}
	if c.Path != "" {
		at = append(at, "path "+c.Path)
	}
	if len(at) > 0 {
		what = fmt.Sprintf("%s (%s)", c.Source, strings.Join(at, ", "))
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
	e.Help = unreachableHelp
	return e
}

var httpReasonPrefix = regexp.MustCompile(`(?s)^.*?(?:Get|Head) "[^"]*": `)

// unreachableReason is git's or the HTTP client's own words for the failure, without the
// source URL they repeat.
func unreachableReason(c *Checkout, cause error) (string, string) {
	if c.Kind == Git {
		for _, row := range out.AsError(cause).Rows {
			if row.Label == "git" {
				return "git", row.Text
			}
		}
		return "git", ""
	}
	return "http", httpReason(cause)
}

func httpReason(err error) string {
	return strings.TrimSpace(httpReasonPrefix.ReplaceAllString(err.Error(), ""))
}

func isOnDisk(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *Store) download(ctx context.Context, url string) ([]byte, error) {
	var buf strings.Builder
	if _, err := s.Fetch.Download(ctx, url, &buf); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}
