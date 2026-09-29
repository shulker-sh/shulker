// Package pack loads the modpacks a project requires from a directory, a git repository, a URL, a
// local archive or a provider, and checks them against the project's platform.
package modpack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/version/dotted"
	"shulker.sh/shulker/internal/version/minecraft"
)

// Kind is where a source lives, which decides how it is fetched.
type Kind string

const (
	Local Kind = "local"
	Git   Kind = "git"
	URL   Kind = "url"
	File  Kind = "file"
	// Hosted is a modpack on a provider, named by neither a source nor a file.
	Hosted Kind = "hosted"
)

// Loaded is a modpack read from its source.
type Loaded struct {
	Name     string
	Source   string
	Kind     Kind
	Dir      string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
	UsesLock bool
	Pin      lock.Modpack
	// Archive is a File modpack's archive, and Overrides the files it lays itself, in place of the
	// override folders a directory has.
	Archive   *packarchive.Archive
	Overrides []packarchive.Override
	// Warnings are what consuming an archive found to mention.
	Warnings   []string
	lockSha256 string
}

type Store struct {
	Cache      *cache.Cache
	ProjectDir string
	Fetch      *fetch.Client
	Log        func(format string, args ...any)
	Warn       func(format string, args ...any)
	// Working shows work under way that clears when it ends, for a step whose outcome is its
	// own line.
	Working func(format string, args ...any)
	// WarnsRawURL has a raw manifest URL warn that its overrides don't come with it: a command
	// that adds or links a source says so once, and the syncs after it stay quiet.
	WarnsRawURL bool
	// Lock is the project's lock, which an archive's entries are rebuilt from.
	Lock *lock.Lock
	// Consume locks what a File modpack's archive holds into its Lock and Manifest, and records
	// in its Pin the files the archive lays itself.
	Consume func(ctx context.Context, l *Loaded) error
	// Obtain picks a Hosted modpack's provider version and puts its archive in the cache,
	// returning the pin that names both.
	Obtain func(ctx context.Context, name string, p manifest.Require) (lock.Modpack, error)

	// mirrored are the git mirrors this run already brought up to date.
	mirrored map[string]bool
}

func (s *Store) isOffline() bool { return s.Fetch != nil && s.Fetch.Offline }

func (s *Store) log(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

func (s *Store) working(format string, args ...any) {
	if s.Working != nil {
		s.Working(format, args...)
	}
}

// Classify tells a source's kind from its form: git URLs and http URLs not ending in .json are git,
// other http URLs are a manifest to fetch, and anything else is a local directory.
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

// Resolve reads a modpack at its source's current state, for locking.
func (s *Store) Resolve(ctx context.Context, name string, p manifest.Require) (*Loaded, error) {
	var err error
	kind := KindOf(p)
	if p.Ref != "" && kind != Git {
		return nil, out.Errorf("modpack-ref", "modpack %s: \"ref\" only applies to git sources", name)
	}
	if p.Path != "" && kind != Git {
		return nil, out.Errorf("modpack-path", "modpack %s: \"path\" only applies to git sources", name)
	}
	l := &Loaded{Name: name, Source: p.Source, Kind: kind, Pin: lock.Modpack{Source: p.Source}}
	switch kind {
	case File:
		l.Source = p.File
		if err := s.resolveArchive(ctx, l, p); err != nil {
			return nil, err
		}
		return l, nil
	case Hosted:
		if err := s.resolveHosted(ctx, l, p); err != nil {
			return nil, err
		}
		return l, nil
	}
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
			return nil, inModpack(name, refNotFound("modpack-ref", p.Ref, p.Source, err))
		}
		l.Pin.Ref, l.Pin.Path = p.Ref, p.Path
		l.Pin.Commit = commit
		export, err := s.export(ctx, packOrigin(name), mirror, commit)
		if err != nil {
			return nil, err
		}
		if l.Dir, err = subfolder(export, p.Path); err != nil {
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
			return nil, inModpack(name, err)
		}
		if err := refuseFiles(name, l.Manifest); err != nil {
			return nil, err
		}
		s.warnRawURL(name, l.Manifest)
		if l.Pin.Sha256, err = s.storeManifest(data); err != nil {
			return nil, err
		}
		if err := s.fetchPackLock(ctx, l); err != nil {
			return nil, err
		}
	}
	if err := l.resolveLocked(p); err != nil {
		return nil, err
	}
	if l.UsesLock {
		l.Pin.UsesLock, l.Pin.LockSha256 = true, l.lockSha256
	}
	return l, nil
}

// Open reads a modpack at the state the lock pinned, and warns when a local one has changed since.
func (s *Store) Open(ctx context.Context, name string, p manifest.Require, pinned lock.Modpack) (*Loaded, string, error) {
	kind := KindOf(p)
	l := &Loaded{Name: name, Source: p.Source, Kind: kind, Pin: pinned}
	warning := ""
	switch kind {
	case File:
		l.Source = p.File
		if err := s.openArchive(ctx, l, p); err != nil {
			return nil, "", err
		}
	case Hosted:
		l.Source = pinned.Provider
		if err := s.openHosted(ctx, l); err != nil {
			return nil, "", err
		}
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
			warning = fmt.Sprintf("modpack %s has changed since the lock; run `shulker lock`.", name)
		}
	case Git:
		if pinned.Commit == "" {
			return nil, "", unlocked(name, "commit")
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
		var err error
		if l.Dir, err = subfolder(dir, pinned.Path); err != nil {
			return nil, "", err
		}
		if err := s.loadDir(l); err != nil {
			return nil, "", err
		}
	case URL:
		if pinned.Sha256 == "" {
			return nil, "", unlocked(name, "hash")
		}
		data, err := os.ReadFile(s.Cache.PackManifest(pinned.Sha256))
		if os.IsNotExist(err) {
			if data, err = s.fetchManifest(ctx, name, p.Source); err != nil {
				return nil, "", err
			}
			if sha256hex(data) != pinned.Sha256 {
				e := out.Errorf("modpack-changed", "modpack %s at %s no longer matches the lock", name, p.Source)
				e.Help = "run `shulker update`"
				return nil, "", e
			}
			if _, err := s.storeManifest(data); err != nil {
				return nil, "", err
			}
		} else if err != nil {
			return nil, "", err
		}
		if l.Manifest, err = manifest.Parse(data); err != nil {
			return nil, "", inModpack(name, err)
		}
		if err := refuseFiles(name, l.Manifest); err != nil {
			return nil, "", err
		}
		if pinned.UsesLock {
			if err := s.openPackLock(ctx, l, pinned.LockSha256); err != nil {
				return nil, "", err
			}
		}
	}
	l.UsesLock = pinned.UsesLock
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
			if l.Pin.Path != "" {
				return out.Errorf("modpack-manifest", "modpack %s: no %s in %s of %s at %s", l.Name, manifest.FileName, l.Pin.Path, l.Source, l.Pin.Commit[:12])
			}
			return out.Errorf("modpack-manifest", "modpack %s: no %s in %s", l.Name, manifest.FileName, l.Source)
		}
		return inModpack(l.Name, err)
	}
	l.Manifest = m
	data, err := os.ReadFile(filepath.Join(l.Dir, lock.FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return inModpack(l.Name, err)
	}
	return l.parseLock(data)
}

func (l *Loaded) parseLock(data []byte) error {
	packLock, err := lock.Parse(data)
	if err != nil {
		return inModpack(l.Name, err)
	}
	l.Lock, l.lockSha256 = packLock, sha256hex(data)
	return nil
}

// fetchPackLock loads the lock beside a fetched manifest, where checkoutURL looks for a
// project's. A source with none there is floating, as a directory without one is.
func (s *Store) fetchPackLock(ctx context.Context, l *Loaded) error {
	data, err := s.download(ctx, lockURL(l.Source))
	if errors.Is(err, fetch.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fetchFailure(l.Name, l.Source, err)
	}
	if err := l.parseLock(data); err != nil {
		return err
	}
	return s.storeFile(s.Cache.PackLock(l.lockSha256), data)
}

// openPackLock loads the lock a URL modpack was locked from, from the cache when it is
// there, so a build off a locked URL modpack needs no network.
func (s *Store) openPackLock(ctx context.Context, l *Loaded, sha string) error {
	data, err := os.ReadFile(s.Cache.PackLock(sha))
	if os.IsNotExist(err) {
		s.log("fetching pack %s lock", l.Name)
		if data, err = s.download(ctx, lockURL(l.Source)); err != nil {
			return fetchFailure(l.Name, l.Source, err)
		}
		if sha256hex(data) != sha {
			e := out.Errorf("modpack-changed", "modpack %s: the %s at %s has changed since this project locked it", l.Name, lock.FileName, l.Source)
			e.Help = "run `shulker update`"
			return e
		}
		if err := s.storeFile(s.Cache.PackLock(sha), data); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return l.parseLock(data)
}

// resolveLocked settles whether this modpack's mods come from its own lock. An
// omitted "locked" follows the source: true when it ships a lock, false when
// there is none beside it.
func (l *Loaded) resolveLocked(p manifest.Require) error {
	switch {
	case l.Lock == nil && p.Locked != nil && *p.Locked:
		e := out.Errorf("modpack-lock-missing", "modpack %s has no %s, so \"locked\": true can't be honoured", l.Name, lock.FileName)
		e.Help = "run `shulker lock` in the modpack, or set locked false"
		return e
	case l.Lock == nil:
		l.UsesLock = false
	case p.Locked != nil:
		l.UsesLock = *p.Locked
	default:
		l.UsesLock = true
	}
	return nil
}

// refuseFiles fails a modpack fetched as a bare manifest that names local files: with no
// directory beside the manifest, there is nothing for their paths to point into.
func refuseFiles(name string, m *manifest.Manifest) error {
	if key, file, ok := localFile(m); ok {
		e := out.Errorf("modpack-url-file", "modpack %s: requires.%s is the local file %s, and a manifest fetched from a URL carries no files", name, key, file)
		e.Help = "serve the modpack from git or a directory instead"
		return e
	}
	return nil
}

func localFile(m *manifest.Manifest) (key, file string, ok bool) {
	for _, key := range slices.Sorted(maps.Keys(m.Requires)) {
		if file := m.Requires[key].File; file != "" {
			return key, file, true
		}
	}
	return "", "", false
}

// WarnRawURL gives a relock that holds a raw-URL modpack at its pin the warning resolving it gives,
// since the lock it writes still takes the modpack from there.
func (s *Store) WarnRawURL(l *Loaded) {
	if l.Kind == URL {
		s.warnRawURL(l.Name, l.Manifest)
	}
}

// warnRawURL is the one warning every command adding or linking a raw manifest URL gives: only
// the manifest and its lock come from there, so the pack's override folders and files never
// arrive. It names the ones the manifest itself points at. The printer drops a repeat, so a
// command that reads the same source twice still says it once.
func (s *Store) warnRawURL(name string, m *manifest.Manifest) {
	if s.Warn == nil || !s.WarnsRawURL {
		return
	}
	rows := ""
	if missing := pointedAt(m); len(missing) > 0 {
		rows = "\nNor are " + strings.Join(missing, " and ")
	}
	s.Warn("%s is a raw manifest URL, so its overrides aren't included.%s\nUse its git URL instead, with path for a pack in a subfolder", name, rows)
}

// pointedAt is what a manifest names in its own directory, beyond the default override folders
// every pack may have: its features' override folders and its icon.
func pointedAt(m *manifest.Manifest) []string {
	if m == nil {
		return nil
	}
	var folders []string
	for _, name := range slices.Sorted(maps.Keys(m.Features)) {
		o := m.Features[name].Overrides
		for _, folder := range []string{o.Both, o.Client, o.Server} {
			if folder != "" && !slices.Contains(folders, folder) {
				folders = append(folders, folder)
			}
		}
	}
	var named []string
	if len(folders) > 0 {
		named = append(named, "the feature overrides "+strings.Join(folders, ", "))
	}
	if m.Icon != "" {
		named = append(named, "the icon "+m.Icon)
	}
	return named
}

func (s *Store) fetchManifest(ctx context.Context, name, url string) ([]byte, error) {
	s.log("fetching pack %s", name)
	var buf strings.Builder
	if _, err := s.Fetch.Download(ctx, url, &buf); err != nil {
		return nil, fetchFailure(name, url, err)
	}
	return []byte(buf.String()), nil
}

// fetchFailure is a URL modpack's manifest or lock that couldn't be fetched, in mirrorFailure's
// shape when the network is why. --offline keeps the plain shape but still counts as the network.
func fetchFailure(name, source string, err error) error {
	if errors.Is(err, fetch.ErrOffline) || !fetch.IsNetwork(err) {
		e := out.Errorf("modpack-fetch", "modpack %s: couldn't fetch %s", name, source)
		e.Rows = []out.Detail{{Label: "http", Text: httpReason(err)}}
		if errors.Is(err, fetch.ErrOffline) {
			return fetch.Unreachable(e)
		}
		return e
	}
	e := out.Errorf("modpack-fetch", "modpack %s: couldn't reach %s", name, source)
	e.Rows = []out.Detail{{Label: "http", Text: httpReason(err)}}
	e.Help = unreachableHelp
	return fetch.Unreachable(e)
}

func (s *Store) storeManifest(data []byte) (string, error) {
	sha := sha256hex(data)
	return sha, s.storeFile(s.Cache.PackManifest(sha), data)
}

func (s *Store) storeFile(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.Write(path, data)
}

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Compatible checks a modpack against the project's platform. A locked modpack
// contributes exact versions, so its own lock has to match; a floating one is
// resolved here and only has to admit the project's versions in its ranges.
func Compatible(l *Loaded, mc string, locked lock.Loader) error {
	if l.UsesLock {
		return compatibleLocked(l, mc, locked)
	}
	pm := l.Manifest
	game, err := minecraft.Parse(mc)
	if err != nil {
		return err
	}
	rng, err := minecraft.ParseRange(pm.Minecraft)
	if err != nil {
		return rangeInvalid(l.Name, "minecraft", err)
	}
	if !rng.Matches(game) {
		return out.Errorf("modpack-mismatch", "modpack %s wants Minecraft %s; this project locked %s", l.Name, pm.Minecraft, mc)
	}
	if pm.Loader.Type != locked.Type {
		return out.Errorf("modpack-mismatch", "modpack %s uses %s; this project uses %s", l.Name, describeLoader(pm.Loader.Type), describeLoader(locked.Type))
	}
	if locked.Type == "" {
		return nil
	}
	lrng, err := dotted.ParseRange(pm.Loader.Version)
	if err != nil {
		return rangeInvalid(l.Name, "loader", err)
	}
	lv, err := dotted.Parse(locked.Version)
	if err != nil {
		return err
	}
	if !lrng.Matches(lv) {
		return out.Errorf("modpack-mismatch", "modpack %s wants %s %s; this project locked %s", l.Name, loader.Title(pm.Loader.Type), pm.Loader.Version, locked.Version)
	}
	return nil
}

func compatibleLocked(l *Loaded, minecraft string, loader lock.Loader) error {
	if l.Lock.Minecraft != minecraft {
		return lockedMismatch(l.Name, "Minecraft "+l.Lock.Minecraft, minecraft)
	}
	if l.Lock.Loader.Type != loader.Type || l.Lock.Loader.Version != loader.Version {
		return lockedMismatch(l.Name, lockedLoaderLabel(l.Lock.Loader), lockedLoaderLabel(loader))
	}
	return nil
}

func lockedMismatch(name, built, locked string) *out.Error {
	e := out.Errorf("modpack-mismatch", "locked modpack %s is built for %s; this project locked %s", name, built, locked)
	e.Help = fmt.Sprintf("unlock it with `shulker set requires.%s.locked false`", name)
	return e
}

func rangeInvalid(name, what string, err error) *out.Error {
	return out.Errorf("manifest-invalid", "modpack %s has a %s range shulker can't read", name, what).WithCause(what, err)
}

func unlocked(name, what string) *out.Error {
	e := out.Errorf("modpack-unlocked", "modpack %s has no %s in the lock", name, what)
	e.Help = "run `shulker update`"
	return e
}

// inModpack names the modpack err came from. Wrapping an *out.Error with fmt.Errorf wouldn't: the
// error renders from the *out.Error inside, whose headline doesn't carry the prefix.
func inModpack(name string, err error) error {
	var e *out.Error
	if errors.As(err, &e) {
		e.Message = "modpack " + name + ": " + e.Message
		return err
	}
	return fmt.Errorf("modpack %s: %w", name, err)
}

func lockedLoaderLabel(l lock.Loader) string {
	if l.Type == "" {
		return "no loader"
	}
	return loader.Title(l.Type) + " " + l.Version
}

func describeLoader(name string) string {
	if name == "" {
		return "no loader"
	}
	return loader.Title(name)
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
		root := filepath.Join(dir, layer)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if rel, _ := filepath.Rel(root, p); !d.IsDir() && !m.Skips(filepath.ToSlash(rel)) {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	for _, r := range m.Requires {
		if r.File == "" {
			continue
		}
		path := filepath.Join(dir, filepath.FromSlash(r.File))
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
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

// Status is where a modpack stands against the lock, as `shulker modpack list` shows it.
type Status struct {
	Name   string `json:"name"`
	Kind   Kind   `json:"kind"`
	Source string `json:"source"`
	Ref    string `json:"ref,omitempty"`
	Path   string `json:"path,omitempty"`
	Pin    string `json:"pin,omitempty"`
	State  string `json:"state"`
}

// Status is where a modpack stands against the lock. Only a local directory or archive is read, to
// see whether it has changed; a remote one is taken as pinned.
func (s *Store) Status(name string, p manifest.Require, pinned lock.Modpack, locked bool) (Status, error) {
	st := Status{Name: name, Kind: KindOf(p), Source: p.Source, Ref: p.Ref, Path: p.Path, State: "unlocked"}
	switch st.Kind {
	case File:
		st.Source = p.File
	case Hosted:
		st.Source = p.Provider
		if locked {
			st.Source = pinned.Provider
		}
	}
	if !locked {
		return st, nil
	}
	st.Pin, st.State = pinned.Label(), "ok"
	if st.Kind == File {
		state, err := s.archiveStatus(p, pinned)
		if err != nil {
			return Status{}, err
		}
		st.State = state
		return st, nil
	}
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
