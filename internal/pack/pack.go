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
	"regexp"
	"sort"
	"strings"

	"github.com/shulker-sh/shulker/internal/fetch"
	"github.com/shulker-sh/shulker/internal/lock"
	"github.com/shulker-sh/shulker/internal/manifest"
	"github.com/shulker-sh/shulker/internal/mcver"
	"github.com/shulker-sh/shulker/internal/out"
)

type Kind string

const (
	Local Kind = "local"
	Git   Kind = "git"
	URL   Kind = "url"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type Loaded struct {
	Name     string
	Source   string
	Kind     Kind
	Dir      string
	Manifest *manifest.Manifest
	Pin      lock.Pack
}

type Store struct {
	CacheDir   string
	ProjectDir string
	Fetch      *fetch.Client
	Log        func(format string, args ...any)
}

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

func Name(p manifest.Pack) (string, error) {
	if p.Name != "" {
		return p.Name, nil
	}
	base := strings.TrimSuffix(p.Source, "/")
	switch Classify(p.Source) {
	case Local:
		base = filepath.Base(filepath.Clean(base))
		if base == "." || base == ".." || base == string(filepath.Separator) {
			return "", out.Errorf("pack-name", "cannot derive a pack name from %q; set \"name\" on the pack entry", p.Source)
		}
	case Git:
		base = strings.TrimSuffix(path.Base(base), ".git")
	case URL:
		base = strings.TrimSuffix(path.Base(base), path.Ext(base))
	}
	name := strings.ToLower(base)
	if !namePattern.MatchString(name) {
		return "", out.Errorf("pack-name", "cannot derive a pack name from %q (got %q); set \"name\" on the pack entry", p.Source, name)
	}
	return name, nil
}

func Names(packs []manifest.Pack) (map[string]manifest.Pack, error) {
	byName := map[string]manifest.Pack{}
	for _, p := range packs {
		name, err := Name(p)
		if err != nil {
			return nil, err
		}
		if other, dup := byName[name]; dup {
			return nil, out.Errorf("pack-name", "packs %q and %q both resolve to the name %q; set \"name\" on one of them", other.Source, p.Source, name)
		}
		byName[name] = p
	}
	return byName, nil
}

func (s *Store) Resolve(ctx context.Context, p manifest.Pack) (*Loaded, error) {
	name, err := Name(p)
	if err != nil {
		return nil, err
	}
	kind := Classify(p.Source)
	if p.Ref != "" && kind != Git {
		return nil, out.Errorf("pack-ref", "pack %s: \"ref\" only applies to git sources", name)
	}
	l := &Loaded{Name: name, Source: p.Source, Kind: kind, Pin: lock.Pack{Name: name}}
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
			return nil, out.Errorf("pack-ref", "pack %s: ref %q not found in %s: %v", name, refOrHead(p.Ref), p.Source, err)
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
			return nil, fmt.Errorf("pack %s: %w", name, err)
		}
		if l.Pin.Sha256, err = s.storeManifest(data); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (s *Store) Open(ctx context.Context, p manifest.Pack, pinned lock.Pack) (*Loaded, string, error) {
	name, err := Name(p)
	if err != nil {
		return nil, "", err
	}
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
			warning = fmt.Sprintf("pack %s has changed since the lock; run `shulker update`", name)
		}
	case Git:
		if pinned.Commit == "" {
			return nil, "", out.Errorf("pack-unlocked", "pack %s has no commit in the lock; run `shulker update`", name)
		}
		dir := s.exportDir(pinned.Commit)
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
			return nil, "", out.Errorf("pack-unlocked", "pack %s has no hash in the lock; run `shulker update`", name)
		}
		data, err := os.ReadFile(s.manifestPath(pinned.Sha256))
		if os.IsNotExist(err) {
			if data, err = s.fetchManifest(ctx, name, p.Source); err != nil {
				return nil, "", err
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != pinned.Sha256 {
				return nil, "", out.Errorf("pack-changed", "pack %s at %s no longer matches the lock; run `shulker update`", name, p.Source)
			}
			if _, err := s.storeManifest(data); err != nil {
				return nil, "", err
			}
		} else if err != nil {
			return nil, "", err
		}
		if l.Manifest, err = manifest.Parse(data); err != nil {
			return nil, "", fmt.Errorf("pack %s: %w", name, err)
		}
	}
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
			return out.Errorf("pack-manifest", "pack %s: no %s in %s", l.Name, manifest.FileName, l.Source)
		}
		return fmt.Errorf("pack %s: %w", l.Name, err)
	}
	l.Manifest = m
	return nil
}

func (s *Store) fetchManifest(ctx context.Context, name, url string) ([]byte, error) {
	s.log("fetching pack %s", name)
	var buf strings.Builder
	if _, err := s.Fetch.Download(ctx, url, &buf); err != nil {
		return nil, out.Errorf("pack-fetch", "pack %s: %v", name, err)
	}
	return []byte(buf.String()), nil
}

func (s *Store) manifestPath(sha string) string {
	return filepath.Join(s.CacheDir, "packs", "url", sha+".json")
}

func (s *Store) storeManifest(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	path := s.manifestPath(sha)
	if _, err := os.Stat(path); err == nil {
		return sha, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return sha, os.WriteFile(path, data, 0o644)
}

func (l *Loaded) Target(name, side string) (*manifest.Target, error) {
	if t, ok := l.Manifest.Targets[name]; ok && t.Side == side {
		return &t, nil
	}
	var matches []string
	for n, t := range l.Manifest.Targets {
		if t.Side == side {
			matches = append(matches, n)
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		t := l.Manifest.Targets[matches[0]]
		return &t, nil
	}
	return nil, out.Errorf("pack-target", "pack %s has several %s targets (%s) and none named %q; rename the project target to match one", l.Name, side, strings.Join(matches, ", "), name)
}

func Compatible(l *Loaded, minecraft string, loader lock.Loader) error {
	pm := l.Manifest
	game, err := mcver.Parse(minecraft)
	if err != nil {
		return err
	}
	rng, err := mcver.ParseRange(pm.Minecraft)
	if err != nil {
		return fmt.Errorf("pack %s minecraft: %w", l.Name, err)
	}
	if !rng.Matches(game) {
		return out.Errorf("pack-mismatch", "pack %s wants minecraft %s; this project locked %s", l.Name, pm.Minecraft, minecraft)
	}
	if pm.Loader.Type != loader.Type {
		return out.Errorf("pack-mismatch", "pack %s uses the %s loader; this project uses %s", l.Name, pm.Loader.Type, loader.Type)
	}
	lrng, err := mcver.ParseRange(pm.Loader.Version)
	if err != nil {
		return fmt.Errorf("pack %s loader version: %w", l.Name, err)
	}
	lv, err := mcver.Parse(loader.Version)
	if err != nil {
		return err
	}
	if !lrng.Matches(lv) {
		return out.Errorf("pack-mismatch", "pack %s wants %s %s; this project locked %s", l.Name, pm.Loader.Type, pm.Loader.Version, loader.Version)
	}
	return nil
}

func dirSha256(dir string, m *manifest.Manifest) (string, error) {
	roots := map[string]bool{}
	for _, t := range m.Targets {
		for _, layer := range t.Overrides {
			roots[layer] = true
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

func (s *Store) Status(p manifest.Pack, pinned lock.Pack, locked bool) (Status, error) {
	name, err := Name(p)
	if err != nil {
		return Status{}, err
	}
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
		if out.CodeOf(err) == "pack-manifest" {
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
