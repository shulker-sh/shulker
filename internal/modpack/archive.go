package modpack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/manual"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
)

// KindOf is where a modpack entry comes from: an archive when it names a file, a provider when it
// names neither a file nor a source, and its source's kind otherwise.
func KindOf(p manifest.Require) Kind {
	switch {
	case p.File != "":
		return File
	case p.Source == "":
		return Hosted
	}
	return Classify(p.Source)
}

// resolveArchive reads a modpack archive as its bytes stand, puts it in the cache, and has
// Consume lock what it holds.
func (s *Store) resolveArchive(ctx context.Context, l *Loaded, p manifest.Require) error {
	pin, err := s.cacheArchive(l.Name, p.File)
	if err != nil {
		return err
	}
	l.Pin = pin
	if l.Archive, err = readArchive(l.Name, p.File, s.Cache.Object(pin.Sha512)); err != nil {
		return err
	}
	if err := s.Consume(ctx, l); err != nil {
		return err
	}
	l.UsesLock = p.Locked == nil || *p.Locked
	l.Pin.UsesLock = l.UsesLock
	return s.layArchive(ctx, l)
}

// openArchive reads a modpack archive at the bytes the lock pinned, from the cache, and rebuilds
// its lock and manifest from the entries the project's lock took from it.
func (s *Store) openArchive(ctx context.Context, l *Loaded, p manifest.Require) error {
	if l.Pin.Sha512 == "" {
		return unlocked(l.Name, "archive hash")
	}
	if !s.Cache.Has(l.Pin.Sha512) {
		path := s.archivePath(p.File)
		got, err := fsutil.SHA512(path)
		if errors.Is(err, os.ErrNotExist) {
			return FileMissing(l.Name, p.File)
		}
		if err != nil {
			return err
		}
		if got != l.Pin.Sha512 {
			e := out.Errorf("modpack-changed", "modpack %s: %s has changed since the lock, and the cache has no copy of the locked bytes", l.Name, p.File)
			e.Help = "run `shulker lock`"
			return e
		}
		if err := s.cacheFile(path, s.Cache.Put); err != nil {
			return err
		}
	}
	return s.openCached(ctx, l, p.File)
}

// openCached reads the archive the lock pinned from the cache, and rebuilds its lock and manifest
// from the entries the project's lock took from it.
func (s *Store) openCached(ctx context.Context, l *Loaded, rel string) error {
	var err error
	if l.Archive, err = readArchive(l.Name, rel, s.Cache.Object(l.Pin.Sha512)); err != nil {
		return err
	}
	l.Manifest, l.Lock = archiveEntries(l.Name, l.Archive, s.Lock)
	return s.layArchive(ctx, l)
}

// resolveHosted picks a hosted modpack's version, puts its archive in the cache, and has Consume
// lock what it holds. The archive is a pin, so the modpack is always locked.
func (s *Store) resolveHosted(ctx context.Context, l *Loaded, p manifest.Require) error {
	pin, err := s.Obtain(ctx, l.Name, p)
	if err != nil {
		return err
	}
	l.Pin, l.Source = pin, pin.Provider
	if l.Archive, err = readArchive(l.Name, pin.Filename, s.Cache.Object(pin.Sha512)); err != nil {
		return err
	}
	if err := s.Consume(ctx, l); err != nil {
		return err
	}
	l.UsesLock, l.Pin.UsesLock = true, true
	return s.layArchive(ctx, l)
}

// openHosted reads a hosted modpack's archive at the version the lock pinned: from the cache, else
// from its url, else from a copy dropped in the downloads folder when its author turned
// distribution off.
func (s *Store) openHosted(ctx context.Context, l *Loaded) error {
	if l.Pin.Sha512 == "" {
		return unlocked(l.Name, "archive hash")
	}
	if !s.Cache.Has(l.Pin.Sha512) {
		if err := s.fetchHosted(ctx, l); err != nil {
			return err
		}
	}
	return s.openCached(ctx, l, l.Pin.Filename)
}

func (s *Store) fetchHosted(ctx context.Context, l *Loaded) error {
	if l.Pin.URL != nil {
		s.log("fetching modpack %s %s", l.Name, l.Pin.VersionNumber)
		if _, err := s.Cache.Ensure(ctx, s.Fetch, *l.Pin.URL, l.Pin.Sha512); err != nil {
			return fetchFailure(l.Name, *l.Pin.URL, err)
		}
		return nil
	}
	dir := filepath.Join(s.ProjectDir, DownloadsDir)
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || e.Name()[0] == '.' {
			continue
		}
		path := filepath.Join(dir, e.Name())
		got, err := fsutil.SHA512(path)
		if err != nil {
			return err
		}
		if got == l.Pin.Sha512 {
			return s.cacheFile(path, s.Cache.PutManual)
		}
	}
	e := out.Errorf("missing-files", "modpack %s %s needs a manual download", l.Name, l.Pin.VersionNumber)
	e.Items = []string{fmt.Sprintf("%s: download %s from %s and place it in %s/", l.Name, l.Pin.Filename, l.Pin.Page, DownloadsDir)}
	manual.Attach(e, []manual.File{{Name: l.Pin.Filename, Page: l.Pin.Page, Sha512: l.Pin.Sha512}})
	return e
}

// DownloadsDir is the project folder files a provider won't serve are dropped into by hand.
const DownloadsDir = "downloads"

// cacheArchive puts the archive at rel in the cache and pins it. One that is gone is served from
// the cache at the bytes the lock pinned, as a gone local file is.
func (s *Store) cacheArchive(name, rel string) (lock.Modpack, error) {
	f, err := os.Open(s.archivePath(rel))
	if errors.Is(err, os.ErrNotExist) {
		if s.Lock != nil {
			if pinned, ok := s.Lock.Modpacks[name]; ok && pinned.File == rel && s.Cache.Has(pinned.Sha512) {
				return lock.Modpack{File: rel, Sha512: pinned.Sha512, Size: pinned.Size}, nil
			}
		}
		return lock.Modpack{}, FileMissing(name, rel)
	}
	if err != nil {
		return lock.Modpack{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return lock.Modpack{}, err
	}
	if !st.Mode().IsRegular() {
		return lock.Modpack{}, NotAFile(name, rel)
	}
	sha, err := s.Cache.Put(f)
	if err != nil {
		return lock.Modpack{}, err
	}
	return lock.Modpack{File: rel, Sha512: sha, Size: st.Size()}, nil
}

func (s *Store) archivePath(rel string) string {
	return filepath.Join(s.ProjectDir, filepath.FromSlash(rel))
}

func (s *Store) cacheFile(path string, put func(io.Reader) (string, error)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = put(f)
	return err
}

// ArchiveMissing is a modpack archive that is gone when the cache can't stand in for it.
func FileMissing(name, rel string) *out.Error {
	e := out.Errorf("local-file-missing", "%s: %s is gone, and the cache has no copy of it", name, rel)
	e.Help = fmt.Sprintf("put the file back at %s, or remove %s from shulker.json", rel, name)
	return e
}

// NotAFile is a local file entry whose path names a folder or something else that isn't a file, where
// only a resource pack or shader may name a folder.
func NotAFile(name, rel string) *out.Error {
	return out.Errorf("file-not-found", "%s: %s is not a file", name, rel)
}

// CheckArchive refuses a file that isn't a modpack archive shulker consumes, before anything is
// done with it.
func CheckArchive(name, path string) error {
	_, err := readArchive(name, filepath.Base(path), path)
	return err
}

// readArchive reads a modpack archive by its content, in whichever format it is.
func readArchive(name, rel, path string) (*packarchive.Archive, error) {
	a, err := packarchive.Read(path)
	if out.CodeOf(err) == "archive-not-modpack" {
		e := out.Errorf("archive-not-modpack", "modpack %s: %s is not a %s modpack", name, rel, packarchive.Titles())
		e.Help = "a modpack's file is " + packarchive.Archives() + "; a directory holding a shulker.json is a source"
		return nil, e
	}
	if err != nil {
		return nil, inModpack(name, err)
	}
	return a, nil
}

// archiveEntries is the lock and manifest a consumed archive had, rebuilt from what the project's
// lock took from it: the entries tagged with the modpack, and the mods it requires by name, each
// naming the provider project it locked from so an unlocked archive's mods resolve as they did. A
// mod the project lists itself is the project's, so the rebuilt lock leaves it out.
func archiveEntries(name string, a *packarchive.Archive, project *lock.Lock) (*manifest.Manifest, *lock.Lock) {
	typ, version := a.Loader.Type, a.Loader.Version
	m := &manifest.Manifest{Name: name, Minecraft: a.Minecraft, Loader: manifest.Loader{Type: typ, Version: version}, Requires: map[string]manifest.Require{}}
	l := lock.New()
	l.Minecraft, l.Loader = m.Minecraft, lock.Loader{Type: typ, Version: version}
	if project == nil {
		return m, l
	}
	for id, mod := range project.Mods {
		if mod.Modpack == name {
			l.Mods[id] = mod
		}
		if slices.Contains(mod.RequiredBy, name) {
			m.Requires[id] = requireFor(mod)
		}
	}
	for _, kind := range manifest.PackKinds {
		for key, p := range project.Packs(kind) {
			if p.Modpack == name {
				l.Packs(kind)[key] = p
			}
		}
	}
	return m, l
}

// requireFor is the requires entry that resolves to the provider project mod was locked from, or
// to the file it was locked from.
func requireFor(mod lock.Mod) manifest.Require {
	if mod.File != "" {
		return manifest.Require{File: mod.File}
	}
	r := manifest.Require{Provider: mod.Provider, Project: mod.Project}
	if mod.Channel != "" && mod.Channel != "release" {
		r.Channel = mod.Channel
	}
	return r
}

// layArchive settles the files an archive lays as its own overrides: every file in its override
// folders but the jars and pack zips, which it lays only where the lock records them as
// unmanaged, and the files in its index the lock records the same way.
func (s *Store) layArchive(ctx context.Context, l *Loaded) error {
	l.Overrides = nil
	for _, o := range l.Archive.Overrides {
		if packarchive.IsModJar(o.Path) || packarchive.IsPackZip(o.Path) {
			if _, ok := l.Pin.Unmanaged[o.Layer+"/"+o.Path]; !ok {
				continue
			}
		}
		l.Overrides = append(l.Overrides, o)
	}
	for _, f := range l.Archive.Files {
		layer := packarchive.LayerFor(f.Side)
		sha, ok := l.Pin.Unmanaged[layer+"/"+f.Path]
		if !ok || sha != f.Hashes["sha512"] || len(f.Downloads) == 0 {
			continue
		}
		path, err := s.Cache.Ensure(ctx, s.Fetch, f.Downloads[0], sha)
		if err != nil {
			return out.Errorf("modpack-download", "modpack %s: couldn't download %s", l.Name, f.Path).WithCause("download", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		l.Overrides = append(l.Overrides, packarchive.Override{Layer: layer, Path: f.Path, Data: data})
	}
	return nil
}

// archiveStatus is where a modpack archive stands against the lock: missing when the file is gone,
// changed when its bytes differ.
func (s *Store) archiveStatus(p manifest.Require, pinned lock.Modpack) (string, error) {
	path := s.archivePath(p.File)
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	if p.File != pinned.File || st.Size() != pinned.Size {
		return "changed", nil
	}
	got, err := fsutil.SHA512(path)
	if err != nil {
		return "", err
	}
	if got != pinned.Sha512 {
		return "changed", nil
	}
	return "ok", nil
}

// FetchArchive downloads url and, when it is a modpack archive, keeps it in the cache at its
// sha512 and returns its path there. Anything else, or nothing at the URL, returns no path, for
// the URL to be read as a git source, unless its name says it is an archive; a download that
// fails otherwise fails the import.
func (s *Store) FetchArchive(ctx context.Context, url string) (string, error) {
	s.log("fetching %s", url)
	tmp, err := s.Cache.TempFile("import")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, err = s.Fetch.Download(ctx, url, tmp)
	tmp.Close()
	isNamedArchive := packarchive.HasArchiveExtension(strings.SplitN(url, "?", 2)[0])
	if errors.Is(err, fetch.ErrNotFound) && !isNamedArchive {
		return "", nil
	}
	if err != nil {
		e := out.Errorf("modpack-fetch", "couldn't download %s", url)
		return "", e.WithCause("http", err)
	}
	if !packarchive.IsArchive(tmp.Name()) {
		if isNamedArchive {
			return "", packarchive.NotArchive(url)
		}
		return "", nil
	}
	file, err := os.Open(tmp.Name())
	if err != nil {
		return "", err
	}
	defer file.Close()
	sha, err := s.Cache.Put(file)
	if err != nil {
		return "", err
	}
	return s.Cache.Object(sha), nil
}
