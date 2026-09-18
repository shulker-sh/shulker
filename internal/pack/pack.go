package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loaderver"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mcver"
	"shulker.sh/shulker/internal/out"
)

type Kind string

const (
	Local Kind = "local"
	Git   Kind = "git"
	URL   Kind = "url"
)

type Loaded struct {
	Name     string
	Source   string
	Kind     Kind
	Dir      string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
	Locked   bool
	Pin      lock.Modpack
}

type Store struct {
	Cache      *cache.Cache
	ProjectDir string
	Fetch      *fetch.Client
	Log        func(format string, args ...any)
}

func (s *Store) offline() bool { return s.Fetch != nil && s.Fetch.Offline }

func (s *Store) log(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

func Classify(source string) Kind {
	if strings.HasPrefix(source, "git@") || strings.HasPrefix(source, "ssh://") || strings.HasPrefix(source, "git://") || strings.HasPrefix(source, "git+") {
		return Git
	}
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "file://") {
		if strings.HasSuffix(strings.TrimSuffix(source, "/"), ".json") {
			return URL
		}
		return Git
	}
	return Local
}

func Key(source string) (string, error) {
	base := strings.TrimSuffix(source, "/")
	switch Classify(source) {
	case Local:
		base = filepath.Base(filepath.Clean(base))
		if base == "." || base == ".." || base == string(filepath.Separator) {
			return "", out.Errorf("modpack-name", "cannot derive a modpack name from %q; pass --as", source)
		}
	case Git:
		base = strings.TrimSuffix(path.Base(base), ".git")
	case URL:
		base = strings.TrimSuffix(path.Base(base), path.Ext(base))
	}
	key := strings.ToLower(base)
	if !manifest.ValidKey(key) {
		return "", out.Errorf("modpack-name", "cannot derive a modpack name from %q (got %q); pass --as", source, key)
	}
	return key, nil
}

func (s *Store) Resolve(ctx context.Context, name string, p manifest.Require) (*Loaded, error) {
	var err error
	kind := Classify(p.Source)
	if p.Ref != "" && kind != Git {
		return nil, out.Errorf("modpack-ref", "modpack %s: \"ref\" only applies to git sources", name)
	}
	l := &Loaded{Name: name, Source: p.Source, Kind: kind, Pin: lock.Modpack{Source: p.Source}}
	switch kind {
	case Local:
		l.Dir = s.localDir(p.Source)
		if err := s.loadDir(l); err != nil {
			return nil, err
		}
		if l.Pin.DirSha256, err = dirSha256(l.Dir, l.Manifest); err != nil {
			return nil, err
		}
	case Git:
		mirror, err := s.ensureMirror(ctx, packOrigin(name), p.Source)
		if err != nil {
			return nil, err
		}
		commit, err := s.revParse(ctx, mirror, p.Ref)
		if err != nil {
			return nil, out.Errorf("modpack-ref", "modpack %s: ref %q not found in %s: %v", name, refOrHead(p.Ref), p.Source, err)
		}
		l.Pin.Ref = p.Ref
		l.Pin.Commit = commit
		if l.Dir, err = s.export(ctx, packOrigin(name), mirror, commit); err != nil {
			return nil, err
		}
		if err := s.loadDir(l); err != nil {
			return nil, err
		}
	case URL:
		data, err := s.fetchManifest(ctx, name, p.Source)
		if err != nil {
			return nil, err
		}
		if l.Manifest, err = manifest.Parse(data); err != nil {
			return nil, fmt.Errorf("modpack %s: %w", name, err)
		}
		if l.Pin.Sha256, err = s.storeManifest(data); err != nil {
			return nil, err
		}
	}
	if err := l.resolveLocked(p); err != nil {
		return nil, err
	}
	if l.Locked {
		l.Pin.Locked = true
		if l.Pin.LockSha256, err = lock.FileSha256(filepath.Join(l.Dir, lock.FileName)); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (s *Store) Open(ctx context.Context, name string, p manifest.Require, pinned lock.Modpack) (*Loaded, string, error) {
	kind := Classify(p.Source)
	l := &Loaded{Name: name, Source: p.Source, Kind: kind, Pin: pinned}
	warning := ""
	switch kind {
	case Local:
		l.Dir = s.localDir(p.Source)
		if err := s.loadDir(l); err != nil {
			return nil, "", err
		}
		current, err := dirSha256(l.Dir, l.Manifest)
		if err != nil {
			return nil, "", err
		}
		if current != pinned.DirSha256 {
			warning = fmt.Sprintf("modpack %s has changed since the lock; run `shulker lock`", name)
		}
	case Git:
		if pinned.Commit == "" {
			return nil, "", out.Errorf("modpack-unlocked", "modpack %s has no commit in the lock; run `shulker update`", name)
		}
		dir := s.Cache.PackSource(pinned.Commit)
		if _, err := os.Stat(dir); err != nil {
			mirror, err := s.ensureMirror(ctx, packOrigin(name), p.Source)
			if err != nil {
				return nil, "", err
			}
			if dir, err = s.export(ctx, packOrigin(name), mirror, pinned.Commit); err != nil {
				return nil, "", err
			}
		}
		l.Dir = dir
		if err := s.loadDir(l); err != nil {
			return nil, "", err
		}
	case URL:
		if pinned.Sha256 == "" {
			return nil, "", out.Errorf("modpack-unlocked", "modpack %s has no hash in the lock; run `shulker update`", name)
		}
		data, err := os.ReadFile(s.Cache.PackManifest(pinned.Sha256))
		if os.IsNotExist(err) {
			if data, err = s.fetchManifest(ctx, name, p.Source); err != nil {
				return nil, "", err
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != pinned.Sha256 {
				return nil, "", out.Errorf("modpack-changed", "modpack %s at %s no longer matches the lock; run `shulker update`", name, p.Source)
			}
			if _, err := s.storeManifest(data); err != nil {
				return nil, "", err
			}
		} else if err != nil {
			return nil, "", err
		}
		if l.Manifest, err = manifest.Parse(data); err != nil {
			return nil, "", fmt.Errorf("modpack %s: %w", name, err)
		}
	}
	l.Locked = pinned.Locked
	return l, warning, nil
}

func (s *Store) localDir(source string) string {
	if filepath.IsAbs(source) {
		return filepath.Clean(source)
	}
	return filepath.Join(s.ProjectDir, source)
}

func (s *Store) loadDir(l *Loaded) error {
	m, err := manifest.Load(filepath.Join(l.Dir, manifest.FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return out.Errorf("modpack-manifest", "modpack %s: no %s in %s", l.Name, manifest.FileName, l.Source)
		}
		return fmt.Errorf("modpack %s: %w", l.Name, err)
	}
	l.Manifest = m
	packLock, err := lock.Load(filepath.Join(l.Dir, lock.FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("modpack %s: %w", l.Name, err)
	}
	l.Lock = packLock
	return nil
}

// resolveLocked settles whether this modpack's mods come from its own lock. An
// omitted "locked" follows the source: true when it ships a lock, false when it
// can't have one, like a manifest fetched from a URL.
func (l *Loaded) resolveLocked(p manifest.Require) error {
	switch {
	case l.Lock == nil && p.Locked != nil && *p.Locked:
		return out.Errorf("modpack-lock-missing", "modpack %s has no %s, so \"locked\": true can't be honoured; run `shulker lock` in the modpack, or set locked false", l.Name, lock.FileName)
	case l.Lock == nil:
		l.Locked = false
	case p.Locked != nil:
		l.Locked = *p.Locked
	default:
		l.Locked = true
	}
	return nil
}

func (s *Store) fetchManifest(ctx context.Context, name, url string) ([]byte, error) {
	s.log("fetching pack %s", name)
	var buf strings.Builder
	if _, err := s.Fetch.Download(ctx, url, &buf); err != nil {
		return nil, out.Errorf("modpack-fetch", "modpack %s: %v", name, err)
	}
	return []byte(buf.String()), nil
}

func (s *Store) storeManifest(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	path := s.Cache.PackManifest(sha)
	if _, err := os.Stat(path); err == nil {
		return sha, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return sha, fsutil.Write(path, data)
}

// Compatible checks a modpack against the project's platform. A locked modpack
// contributes exact versions, so its own lock has to match; a floating one is
// resolved here and only has to admit the project's versions in its ranges.
func Compatible(l *Loaded, minecraft string, loader lock.Loader) error {
	if l.Locked {
		return compatibleLocked(l, minecraft, loader)
	}
	pm := l.Manifest
	game, err := mcver.Parse(minecraft)
	if err != nil {
		return err
	}
	rng, err := mcver.ParseRange(pm.Minecraft)
	if err != nil {
		return fmt.Errorf("modpack %s minecraft: %w", l.Name, err)
	}
	if !rng.Matches(game) {
		return out.Errorf("modpack-mismatch", "modpack %s wants minecraft %s; this project locked %s", l.Name, pm.Minecraft, minecraft)
	}
	if pm.Loader.Type != loader.Type {
		return out.Errorf("modpack-mismatch", "modpack %s uses %s; this project uses %s", l.Name, describeLoader(pm.Loader.Type), describeLoader(loader.Type))
	}
	if loader.Type == "" {
		return nil
	}
	lrng, err := loaderver.ParseRange(pm.Loader.Version)
	if err != nil {
		return fmt.Errorf("modpack %s loader version: %w", l.Name, err)
	}
	lv, err := loaderver.Parse(loader.Version)
	if err != nil {
		return err
	}
	if !lrng.Matches(lv) {
		return out.Errorf("modpack-mismatch", "modpack %s wants %s %s; this project locked %s", l.Name, pm.Loader.Type, pm.Loader.Version, loader.Version)
	}
	return nil
}

func compatibleLocked(l *Loaded, minecraft string, loader lock.Loader) error {
	if l.Lock.Minecraft != minecraft {
		return out.Errorf("modpack-mismatch", "locked modpack %s is built for minecraft %s; this project locked %s. Unlock it with `shulker set requires.%s.locked false`", l.Name, l.Lock.Minecraft, minecraft, l.Name)
	}
	if l.Lock.Loader.Type != loader.Type || l.Lock.Loader.Version != loader.Version {
		return out.Errorf("modpack-mismatch", "locked modpack %s is built for %s; this project locked %s. Unlock it with `shulker set requires.%s.locked false`", l.Name, lockedLoaderLabel(l.Lock.Loader), lockedLoaderLabel(loader), l.Name)
	}
	return nil
}

func lockedLoaderLabel(l lock.Loader) string {
	if l.Type == "" {
		return "no loader"
	}
	return l.Type + " " + l.Version
}

func describeLoader(name string) string {
	if name == "" {
		return "no loader"
	}
	return name
}

func dirSha256(dir string, m *manifest.Manifest) (string, error) {
	roots := map[string]bool{"overrides": true, "client-overrides": true, "server-overrides": true}
	for name, f := range m.Features {
		for _, folder := range []string{f.Overrides.Both, f.Overrides.Client, f.Overrides.Server} {
			if folder != "" {
				roots[folder] = true
			}
		}
		if f.Overrides == (manifest.FeatureOverrides{}) {
			roots[name+"-overrides"] = true
		}
	}
	var files []string
	for layer := range roots {
		err := filepath.WalkDir(filepath.Join(dir, layer), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if !d.IsDir() {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	files = append(files, filepath.Join(dir, manifest.FileName))
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		rel, _ := filepath.Rel(dir, f)
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func refOrHead(ref string) string {
	if ref == "" {
		return "HEAD"
	}
	return ref
}

type Status struct {
	Name   string `json:"name"`
	Kind   Kind   `json:"kind"`
	Source string `json:"source"`
	Ref    string `json:"ref,omitempty"`
	Pin    string `json:"pin,omitempty"`
	State  string `json:"state"`
}

func (s *Store) Status(name string, p manifest.Require, pinned lock.Modpack, locked bool) (Status, error) {
	st := Status{Name: name, Kind: Classify(p.Source), Source: p.Source, Ref: p.Ref, State: "unlocked"}
	if !locked {
		return st, nil
	}
	st.Pin, st.State = pinned.Label(), "ok"
	if st.Kind != Local {
		return st, nil
	}
	l := &Loaded{Name: name, Source: p.Source, Kind: Local, Dir: s.localDir(p.Source)}
	if err := s.loadDir(l); err != nil {
		if out.CodeOf(err) == "modpack-manifest" {
			st.State = "missing"
			return st, nil
		}
		return Status{}, err
	}
	current, err := dirSha256(l.Dir, l.Manifest)
	if err != nil {
		return Status{}, err
	}
	if current != pinned.DirSha256 {
		st.State = "changed"
	}
	return st, nil
}
