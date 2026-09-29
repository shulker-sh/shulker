// Package audit reports what in a project or instance deserves a closer look: files their provider
// no longer has, entries that download from outside their provider or that it files under another
// project, files no provider published, installed jars that no longer match the lock, and versions
// younger than security.minReleaseAge.
package audit

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/security"
	"shulker.sh/shulker/internal/sync"
	"shulker.sh/shulker/internal/takedown"
)

// Where an unpublished file comes from.
const (
	FromFile     = "file"
	FromOverride = "override"
	FromDownload = "download"
)

// What is wrong with an installed jar.
const (
	Changed  = "changed"
	Unlisted = "unlisted"
)

// Unpublished is a jar or pack that reaches the game without any provider.
type Unpublished struct {
	// Key is the lock entry that places it, empty for a file an override folder lays.
	Key string `json:"key,omitempty"`
	// Path is where it lands in the game directory.
	Path string `json:"path"`
	From string `json:"from"`
	// Source is the local file, the override folder or the URL it comes from.
	Source  string `json:"source"`
	Modpack string `json:"modpack,omitempty"`
}

// Installed is a jar in a built directory's mods/ that the lock doesn't account for: one whose
// bytes changed since shulker placed it, or one no lock entry or override placed.
type Installed struct {
	Dir     string `json:"dir"`
	Path    string `json:"path"`
	Key     string `json:"key,omitempty"`
	Problem string `json:"problem"`
}

// Dir is a directory a side was built into.
type Dir struct {
	Path string
	Side string
}

// Options says what an audit covers and when it runs.
type Options struct {
	// Keys narrows every check to these entries and the entries each named modpack brings.
	Keys []string
	// Dirs are the built directories whose mods/ is checked.
	Dirs          []Dir
	MinReleaseAge time.Duration
	Now           time.Time
}

// Report is what an audit found, one list per check.
type Report struct {
	Keys []string `json:"keys,omitempty"`
	// Takedowns are the files their provider no longer has.
	Takedowns  []takedown.File  `json:"takedowns"`
	Provenance []build.Mismatch `json:"provenance"`
	// Moved are the files their provider files under another project than the lock names.
	Moved []takedown.File `json:"moved"`
	// Skipped are the providers the takedown check couldn't ask, whose files went unchecked.
	Skipped       []takedown.Skipped `json:"skipped"`
	Unpublished   []Unpublished      `json:"unpublished"`
	Installed     []Installed        `json:"installed"`
	Young         []resolve.Young    `json:"young"`
	MinReleaseAge int                `json:"minReleaseAge"`
}

// Fails reports whether the audit found something that fails it: unpublished files, installed
// jars and young versions are listed without failing it, since a pack may reasonably have them.
func (r *Report) Fails() bool { return len(r.Takedowns)+len(r.Provenance)+len(r.Moved) > 0 }

// Run audits b's lock. Only the takedown check goes online, one request per provider.
func Run(ctx context.Context, b *build.Builder, o Options) (*Report, error) {
	scope, err := scopeOf(b.Lock, o.Keys)
	if err != nil {
		return nil, err
	}
	r := &Report{Keys: o.Keys, Takedowns: []takedown.File{}, Provenance: []build.Mismatch{}, Moved: []takedown.File{}, Unpublished: []Unpublished{}, Installed: []Installed{}, Young: []resolve.Young{}, MinReleaseAge: security.Days(o.MinReleaseAge)}
	for _, m := range build.Mismatches(b.Providers, b.Lock) {
		if scope.has(m.Key) {
			r.Provenance = append(r.Provenance, m)
		}
	}
	var entries []takedown.Entry
	for _, e := range takedown.Entries(b.Lock) {
		if scope.has(e.Key) {
			entries = append(entries, e)
		}
	}
	checked := takedown.Check(ctx, b.Providers, b.Cache, entries)
	r.Takedowns, r.Moved, r.Skipped = append(r.Takedowns, checked.With(takedown.Gone)...), append(r.Moved, checked.With(takedown.Moved)...), checked.Skipped
	for _, y := range resolve.YoungEntries(b.Lock, o.MinReleaseAge, o.Now) {
		if scope.has(y.Key) {
			r.Young = append(r.Young, y)
		}
	}
	r.Unpublished = unpublishedEntries(b.Lock, scope)
	laid, err := laidJars(b, scope)
	if err != nil {
		return nil, err
	}
	r.Unpublished = append(r.Unpublished, laid...)
	for _, d := range o.Dirs {
		found, err := installed(b.Lock, d, scope)
		if err != nil {
			return nil, err
		}
		r.Installed = append(r.Installed, found...)
	}
	return r, nil
}

// scope is the entries an audit covers; a nil one covers everything.
type scope map[string]bool

func (s scope) has(key string) bool { return s == nil || s[key] }

// scopeOf is the named keys and every entry a named modpack brings, refusing a key the lock
// doesn't hold.
func scopeOf(l *lock.Lock, keys []string) (scope, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	modpackOf := map[string]string{}
	for key, m := range l.Mods {
		modpackOf[key] = m.Modpack
	}
	for _, section := range l.PackSections() {
		for key, p := range section {
			modpackOf[key] = p.Modpack
		}
	}
	for key := range l.Modpacks {
		modpackOf[key] = ""
	}
	s := scope{}
	for _, key := range keys {
		if _, ok := modpackOf[key]; !ok {
			e := out.Errorf("mod-not-found", "%s is not in the lock", key)
			e.Candidates, e.Given = slices.Sorted(maps.Keys(modpackOf)), key
			return nil, e
		}
		s[key] = true
	}
	for key, pack := range modpackOf {
		if pack != "" && s[pack] {
			s[key] = true
		}
	}
	return s, nil
}

// unpublishedEntries are the lock's mods and packs that name no provider: local files, and files
// downloaded from a URL of their own.
func unpublishedEntries(l *lock.Lock, s scope) []Unpublished {
	var found []Unpublished
	add := func(key, path, file, provider, modpack string, u *string) {
		if provider != "" || !s.has(key) {
			return
		}
		switch {
		case file != "":
			found = append(found, Unpublished{Key: key, Path: path, From: FromFile, Source: file, Modpack: modpack})
		case u != nil:
			found = append(found, Unpublished{Key: key, Path: path, From: FromDownload, Source: build.HostOf(*u), Modpack: modpack})
		}
	}
	for _, key := range slices.Sorted(maps.Keys(l.Mods)) {
		m := l.Mods[key]
		add(key, "mods/"+m.Filename, m.File, m.Provider, m.Modpack, m.URL)
	}
	for _, kind := range manifest.PackKinds {
		section := l.Packs(kind)
		for _, key := range slices.Sorted(maps.Keys(section)) {
			p := section[key]
			path := p.Path(kind)
			if kind == manifest.TypeDatapack {
				path = "datapacks/" + p.Filename
			}
			add(key, path, p.File, p.Provider, p.Modpack, p.URL)
		}
	}
	return found
}

// laidJars are the jars and packs the override folders of every side lay, once each; narrowed,
// only those a named modpack brings.
func laidJars(b *build.Builder, s scope) ([]Unpublished, error) {
	var found []Unpublished
	seen := map[string]bool{}
	for _, side := range b.Manifest.Sides() {
		laid, err := b.LaidJars(side)
		if err != nil {
			return nil, err
		}
		for _, l := range laid {
			if s != nil && !s[l.Modpack] || seen[l.Folder+"/"+l.Path] {
				continue
			}
			seen[l.Folder+"/"+l.Path] = true
			found = append(found, Unpublished{Path: l.Path, From: FromOverride, Source: l.Folder, Modpack: l.Modpack})
		}
	}
	return found, nil
}

// installed checks each jar in d's mods/: one named like a lock entry against the entry's sha512,
// one an override placed against the hash the build recorded, and any other as unlisted.
// Narrowed, only the named entries' jars are checked.
func installed(l *lock.Lock, d Dir, s scope) ([]Installed, error) {
	entries, err := os.ReadDir(filepath.Join(d.Path, "mods"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	byFilename := map[string]string{}
	for key, m := range l.Mods {
		if m.PlacedOn(d.Side) && !m.IsPending() {
			byFilename[m.Filename] = key
		}
	}
	state, _ := instance.ReadState(d.Path)
	var found []Installed
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".jar") {
			continue
		}
		rel := "mods/" + e.Name()
		abs := filepath.Join(d.Path, "mods", e.Name())
		key, locked := byFilename[e.Name()]
		recorded, placed := state.Files[rel]
		switch {
		case locked:
			if !s.has(key) {
				continue
			}
			sum, err := hashFile(sha512.New(), abs)
			if err != nil {
				return nil, err
			}
			if sum != l.Mods[key].Sha512 {
				found = append(found, Installed{Dir: d.Path, Path: rel, Key: key, Problem: Changed})
			}
		case s != nil:
		case placed:
			sum, err := hashFile(sha256.New(), abs)
			if err != nil {
				return nil, err
			}
			if sum != recorded {
				found = append(found, Installed{Dir: d.Path, Path: rel, Problem: Changed})
			}
		default:
			found = append(found, Installed{Dir: d.Path, Path: rel, Problem: Unlisted})
		}
	}
	return found, nil
}

func hashFile(h hash.Hash, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ProjectDirs are the directories p's sides were built into, each that holds a build.
func ProjectDirs(b *build.Builder) []Dir {
	var dirs []Dir
	for _, side := range b.Manifest.Sides() {
		dir := b.Target(side, "")
		if _, err := os.Stat(instance.StatePath(dir)); err == nil {
			dirs = append(dirs, Dir{Path: dir, Side: side})
		}
	}
	return dirs
}

// Instance is the project a registered instance runs on and the directory it is built into, found
// without the network: the instance itself when it builds in place, else the project its last
// sync built from.
func Instance(c *cache.Cache, entry project.InstanceEntry) (*project.Project, Dir, error) {
	p, side, inPlace, err := sync.InPlaceProject(entry.Dir)
	if err != nil {
		return nil, Dir{}, err
	}
	if !inPlace {
		if p, err = syncedProject(c, entry); err != nil {
			return nil, Dir{}, err
		}
		side = entry.Side
	}
	return p, Dir{Path: entry.Dir, Side: side}, nil
}

// syncedProject is the project a synced instance was last built from: its local source, or the
// checkout of a remote source that its last build recorded.
func syncedProject(c *cache.Cache, entry project.InstanceEntry) (*project.Project, error) {
	var dir string
	if modpack.Classify(entry.Source) == modpack.Local {
		dir = filepath.Join(entry.Source, filepath.FromSlash(entry.Path))
	} else {
		state, _ := instance.ReadState(entry.Dir)
		switch {
		case state.Commit != "":
			dir = filepath.Join(c.PackSource(state.Commit), filepath.FromSlash(entry.Path))
		case state.Sha256 != "":
			dir = c.ProjectCheckout(state.Sha256)
		}
	}
	if dir == "" {
		return nil, notSynced(entry)
	}
	if _, err := os.Stat(filepath.Join(dir, lock.FileName)); err != nil {
		return nil, notSynced(entry)
	}
	return project.Open(dir)
}

func notSynced(entry project.InstanceEntry) error {
	e := out.Errorf("not-synced", "%s has no lock from its last sync to audit", entry.Label())
	e.Help = "run `shulker sync -i " + entry.ID + "` first"
	return e
}
