// Package build writes a project's side into a directory: mods, packs, overrides and rendered
// config, tracking what it wrote so a rebuild can tell its own files from the player's edits.
package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/integrations"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/managed"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/server"
	"shulker.sh/shulker/internal/version/minecraft"
	"shulker.sh/shulker/schema"
)

const (
	StateDir          = ".shulker"
	StateFile         = "state.json"
	TemplateSuffix    = ".tmpl"
	VanillaServerFile = loader.VanillaServerFile
	EulaFile          = "eula.txt"
)

func StatePath(dir string) string {
	return filepath.Join(dir, StateDir, StateFile)
}

// Origin is where the project a build came from was synced from: a source, a ref, path and commit,
// or an archive's sha256. It is empty for a build of the local project.
type Origin struct {
	Source string `json:"source,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Path   string `json:"path,omitempty"`
	Commit string `json:"commit,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// State is .shulker/state.json: what the last build wrote into a directory, and the hash of each
// file it owns.
type State struct {
	Schema string `json:"$schema"`
	Side   string `json:"side"`
	Origin
	BuiltAt       string                       `json:"builtAt"`
	LockSha256    string                       `json:"lockSha256"`
	Minecraft     string                       `json:"minecraft,omitempty"`
	Loader        string                       `json:"loader,omitempty"`
	LoaderVersion string                       `json:"loaderVersion,omitempty"`
	Files         map[string]string            `json:"files"`
	Values        map[string]map[string]string `json:"managedValues,omitempty"`
	Links         []string                     `json:"links,omitempty"`
	// Packs is the file name each resource pack was placed under, so a renamed
	// pack's entry in the enabled list can follow it.
	Packs map[string]string `json:"packs,omitempty"`
	// InstalledLoader is the loader its own installer set up in the dir; the installer's files aren't tracked.
	InstalledLoader *InstalledLoader `json:"installedLoader,omitempty"`
	// LauncherImage is the hash of the instance image shulker last wrote into the launcher.
	LauncherImage string `json:"launcherImage,omitempty"`
}

// InstalledLoader is a loader that its own installer set up, rather than shulker.
type InstalledLoader struct {
	Type    string `json:"type"`
	Version string `json:"version"`
}

func (s State) recordedKeys(rel string) []string {
	keys := make([]string, 0, len(s.Values[rel]))
	for k := range s.Values[rel] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// isUntouched reports whether the file at abs, hashing to current, is still what the last build
// left there. A file the last build merged key by key is recorded by its values rather than its
// bytes, so it is untouched only while it holds exactly those keys and values.
func (s State) isUntouched(rel, abs, current string) (bool, error) {
	values, merged := s.Values[rel]
	if !merged {
		return current == s.Files[rel], nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return false, err
	}
	return maps.Equal(map[string]string(parseProperties(data)), values), nil
}

func (s *State) record(rel string, f ownedFile) {
	if s.Values == nil {
		s.Values = map[string]map[string]string{}
	}
	s.Values[rel] = f.values()
}

// Report is what a build did, file by file.
type Report struct {
	Side      string   `json:"side"`
	Dir       string   `json:"dir"`
	Written   []string `json:"written"`
	Unchanged int      `json:"unchanged"`
	Kept      []string `json:"kept"`
	Removed   []string `json:"removed"`
	Linked    []string `json:"linked"`
	Moved     []string `json:"moved"`
	MovedBack []string `json:"movedBack"`
	Conflicts []string `json:"conflicts"`
	// KeptConflicts are the conflicts Options.KeepConflicts left as the player had them.
	KeptConflicts []string `json:"keptConflicts,omitempty"`
	Excluded      []string `json:"excluded"`
	Warnings      []string `json:"-"`
	// State is why the directory's state file was read as empty, warned apart from Warnings since
	// its fix is a command.
	State   *StateError `json:"-"`
	Forced  bool        `json:"forced"`
	History string      `json:"history,omitempty"`
	// InstalledLoader is set when the loader's own installer ran into the dir after the build.
	InstalledLoader *InstalledLoader `json:"installedLoader,omitempty"`
}

// Options change how a build runs. Dir builds somewhere other than the side's build directory, and
// Features turns features on or off over their defaults.
type Options struct {
	Force       bool
	Dir         string
	NoDataLinks bool
	NoHistory   bool
	OS          string
	NoOS        bool
	Features    map[string]bool
	Origin      Origin
	// NoLauncher leaves the server launcher out, for an export that ships none of it.
	NoLauncher bool
	// NoEULA leaves eula.txt out, for an export: the acceptance is this user's, not the pack's.
	NoEULA bool
	// ProjectVersion stands in for the manifest's version in the project's ${project.version}, for an
	// export given a version of its own.
	ProjectVersion string
	// BeforeModChange runs once, before a build that adds, replaces or removes a mod writes
	// anything.
	BeforeModChange func() error
	// KeepConflicts leaves a conflicting file as it is on disk and builds everything else, recording
	// the file at its new source version so it counts as edited in place from then on.
	KeepConflicts bool
}

// Builder builds, diffs and exports one project from its manifest, lock and cached files.
type Builder struct {
	Dir      string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
	LockPath string
	Cache    *cache.Cache
	Packs    []*modpack.Loaded
	// Providers names the hosts the lock's entries come from, for messages that say who, and an
	// export asks them which files the archive's format can list.
	Providers provider.Providers
	Fetch     *fetch.Client
	Log       func(format string, args ...any)
	// EULA is whether this user accepted the Minecraft EULA in config.json, which a server build
	// writes to eula.txt unless the player wrote one there themselves. A manifest can't accept it on
	// anyone's behalf.
	EULA bool
}

type source struct {
	content    content
	origin     string
	pack       string
	feature    string
	isTemplate bool
	// managed is the owned file an override replaced, remembered so pull can
	// still adopt the keys the manifest sets in it.
	managed ownedFile
}

// content is what a desired file holds: bytes from the cache, bytes in hand, or
// an owned file shulker merges key by key with what is on disk.
type content interface {
	hash(b *Builder) (string, error)
	bytes(b *Builder, abs string, m keyMerge) ([]byte, error)
}

type cached struct{ sha512 string }

func (c cached) hash(b *Builder) (string, error) {
	data, err := os.ReadFile(b.Cache.Object(c.sha512))
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func (c cached) bytes(b *Builder, _ string, _ keyMerge) ([]byte, error) {
	return os.ReadFile(b.Cache.Object(c.sha512))
}

type literal struct{ data []byte }

func (l literal) hash(*Builder) (string, error) { return sha256Hex(l.data), nil }

func (l literal) bytes(*Builder, string, keyMerge) ([]byte, error) { return l.data, nil }

type ownedContent struct{ ownedFile }

func (o ownedContent) hash(*Builder) (string, error) {
	return sha256Hex(canonicalValues(o.values())), nil
}

func (o ownedContent) bytes(_ *Builder, abs string, m keyMerge) ([]byte, error) {
	existing, err := os.ReadFile(abs)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return o.render(existing, m.kept, m.dropped)
}

func fromCache(sha512 string) source { return source{content: cached{sha512}} }

func ownedSource(f ownedFile) source { return source{content: ownedContent{f}} }

// owned is the file shulker merges key by key, or nil for a whole file.
func (s source) owned() ownedFile {
	if o, ok := s.content.(ownedContent); ok {
		return o.ownedFile
	}
	return nil
}

func (s source) props() (propsFile, bool) {
	pf, ok := s.owned().(propsFile)
	return pf, ok
}

func (s source) isCached() bool {
	_, ok := s.content.(cached)
	return ok
}

// data is a whole file's bytes in hand, or nil for a cached or owned one.
func (s source) data() []byte {
	if l, ok := s.content.(literal); ok {
		return l.data
	}
	return nil
}

type fileState string

const (
	stateWrite     fileState = "write"
	stateUnchanged fileState = "unchanged"
	stateKept      fileState = "kept"
	stateConflict  fileState = "conflict"
	stateUntracked fileState = "untracked"
	stateOrphan    fileState = "orphan"
	stateRemove    fileState = "remove"
)

type planned struct {
	rel   string
	state fileState
	src   source
	hash  string
	merge keyMerge
	// isForced is set where only --force let the plan overwrite or remove
	// something the player changed.
	isForced bool
}

type ownedFile interface {
	keys() []string
	values() map[string]string
	existingValues(existing []byte) map[string]string
	render(existing []byte, kept, dropped map[string]bool) ([]byte, error)
}

type keyMerge struct {
	kept       map[string]bool
	dropped    map[string]bool
	overrode   []string
	hasChanged bool
	isForced   bool
}

func mergeKeys(f ownedFile, existing []byte, recorded map[string]string, recordedKeys []string, force bool) keyMerge {
	m := keyMerge{kept: map[string]bool{}, dropped: map[string]bool{}}
	desired := f.values()
	current := f.existingValues(existing)
	for _, k := range f.keys() {
		want := desired[k]
		have, present := current[k]
		last, known := recorded[k]
		switch {
		case !present:
			m.hasChanged = true
		case have == want:
		case !known, have == last:
			m.hasChanged = true
		case force:
			m.hasChanged = true
			m.isForced = true
		case want == last:
			m.kept[k] = true
		default:
			m.overrode = append(m.overrode, k)
			m.hasChanged = true
		}
	}
	for _, k := range recordedKeys {
		if _, still := desired[k]; still {
			continue
		}
		if _, present := current[k]; present {
			m.dropped[k] = true
			m.hasChanged = true
		}
	}
	return m
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Target is the directory a build writes into: the caller's own where it named one, else the side's
// build directory. Callers resolve it to read what the directory itself says about a build.
func (b *Builder) Target(side, dir string) string {
	if dir != "" {
		return dir
	}
	return filepath.Join(b.Dir, b.Manifest.BuildDir(side))
}

// Build writes side into its target directory, keeping the player's edits to files shulker owns
// and failing on a conflict unless opts.Force is set.
func (b *Builder) Build(side string, opts Options) (*Report, error) {
	dir := b.Target(side, opts.Dir)
	inPlace := sameDir(dir, b.Dir)
	report := &Report{Side: side, Dir: dir, Written: []string{}, Kept: []string{}, Removed: []string{}, Linked: []string{}, Moved: []string{}, MovedBack: []string{}, Conflicts: []string{}, Excluded: []string{}, Warnings: []string{}, Forced: opts.Force}
	desired, dirs, err := b.collect(side, opts, report)
	if err != nil {
		return nil, err
	}
	if inPlace {
		if err := checkReserved(side, desired, dirs); err != nil {
			return nil, err
		}
	}
	if opts.NoDataLinks || inPlace {
		dirs = nil
	}
	prev, stateErr := ReadState(dir)
	report.State = stateErr
	next := State{Side: side, Origin: opts.Origin, Files: map[string]string{}, InstalledLoader: prev.InstalledLoader, LauncherImage: prev.LauncherImage, Packs: b.placedPackNames(desired)}
	links, err := b.planLinks(dir, side, dirs, prev, report)
	if err != nil {
		return nil, err
	}
	next.Links = dirs

	plans, err := b.plan(dir, desired, prev, opts.Force)
	if err != nil {
		return nil, err
	}
	var writes []string
	for _, f := range plans {
		switch f.state {
		case stateWrite:
			writes = append(writes, f.rel)
		case stateUnchanged:
			report.Unchanged++
		case stateKept:
			if f.src.owned() == nil {
				report.Kept = append(report.Kept, f.rel)
			}
		case stateConflict, stateUntracked:
			conflict := f.rel + " (changed in place and in the source)"
			if f.state == stateUntracked {
				conflict = f.rel + " (not written by shulker)"
			}
			if !opts.KeepConflicts {
				report.Conflicts = append(report.Conflicts, conflict)
				continue
			}
			report.KeptConflicts = append(report.KeptConflicts, conflict)
		case stateOrphan:
			report.Kept = append(report.Kept, f.rel+" (edited; no longer in source)")
			continue
		case stateRemove:
			report.Removed = append(report.Removed, f.rel)
			continue
		}
		next.Files[f.rel] = f.hash
		if f.src.owned() != nil {
			next.record(f.rel, f.src.owned())
			for _, k := range sortedKeys(f.merge.kept) {
				report.Kept = append(report.Kept, f.rel+" "+k+" (edited in place)")
			}
			for _, k := range f.merge.overrode {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %s was edited in place and changed in the manifest; the manifest value was written", f.rel, k))
			}
		}
	}
	if len(report.Conflicts) > 0 {
		e := out.Errorf("build-conflict", "%s: %d file(s) changed in the output directory and in the source", side, len(report.Conflicts))
		e.Help = "run `shulker diff`, or `shulker build --force` to overwrite"
		e.Items = report.Conflicts
		return report, e
	}
	if opts.BeforeModChange != nil && changesMods(writes, report.Removed) {
		if err := opts.BeforeModChange(); err != nil {
			return nil, err
		}
	}
	if inPlace && !opts.NoHistory && planDrift(plans) {
		if err := b.takeHistory(dir, side, "build", len(writes), len(report.Removed), report); err != nil {
			return nil, err
		}
	}
	merges := map[string]keyMerge{}
	for _, f := range plans {
		merges[f.rel] = f.merge
	}
	removed := slices.Clone(report.Removed)
	// Links first: a server's datapacks are written through its world's link.
	if err := b.applyLinks(dir, side, links, report); err != nil {
		return nil, err
	}
	for _, rel := range writes {
		if err := b.write(filepath.Join(dir, filepath.FromSlash(rel)), desired[rel], merges[rel]); err != nil {
			return nil, err
		}
		report.Written = append(report.Written, rel)
	}
	for _, rel := range removed {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		// The bytes reach the cache before the file leaves the disk, so a history
		// entry that left them to the cache can still be restored offline.
		if err := b.Cache.Ingest(path); err != nil {
			return nil, err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	next.BuiltAt = time.Now().UTC().Format(time.RFC3339)
	next.Minecraft, next.Loader, next.LoaderVersion = b.Lock.Minecraft, b.Lock.Loader.Type, b.Lock.Loader.Version
	if next.LockSha256, err = lock.FileSha256(b.LockPath); err != nil {
		return nil, err
	}
	if err := b.saveState(dir, next); err != nil {
		return nil, err
	}
	return report, nil
}

func changesMods(writes, removed []string) bool {
	isMod := func(rel string) bool { return strings.HasPrefix(rel, "mods/") }
	return slices.ContainsFunc(writes, isMod) || slices.ContainsFunc(removed, isMod)
}

func (b *Builder) collect(side string, opts Options, report *Report) (map[string]source, []string, error) {
	desired := map[string]source{}
	dirs := dataDirs(side, "world")
	dir := b.Target(side, opts.Dir)
	cond := b.conditions(opts)
	sel := b.selectMods(cond)
	report.Excluded = append(report.Excluded, sel.excluded...)
	report.Warnings = append(report.Warnings, sel.warnings...)
	placed := map[string]bool{}
	for id, m := range b.Lock.Mods {
		if !sel.included[id] || !m.PlacedOn(side) {
			continue
		}
		if !b.Cache.Has(m.Sha512) {
			return nil, nil, notInstalled(id)
		}
		desired["mods/"+m.Filename] = fromCache(m.Sha512)
		placed[b.Lock.JarID(id)] = true
	}
	vars := templateVars(b.Manifest, b.Lock, side)
	if opts.ProjectVersion != "" {
		vars["project.version"] = opts.ProjectVersion
	}
	levelName := "world"
	var shipped string
	if side == "server" {
		var err error
		eula := b.EULA && !opts.NoEULA && !handwritten(dir, EulaFile)
		if levelName, err = b.collectServer(desired, vars, cond, opts.NoLauncher, eula, report); err != nil {
			return nil, nil, err
		}
		dirs = dataDirs(side, levelName)
	}
	if err := b.collectDatapacks(side, levelName, cond, desired, report); err != nil {
		return nil, nil, err
	}
	if side == "client" {
		if err := b.collectPacks(cond, desired, report); err != nil {
			return nil, nil, err
		}
		present := integrations.Match(placed, b.Manifest.Integrations)
		if err := b.chooseShader(side, opts, desired, present, report); err != nil {
			return nil, nil, err
		}
		b.reportUnloadableShaders(desired, present, report)
		var err error
		if shipped, err = b.shippedPackList(side, cond, vars); err != nil {
			return nil, nil, err
		}
		if err := b.collectClient(side, opts, desired, vars, shipped, report); err != nil {
			return nil, nil, err
		}
		if loader.Running(b.Lock).MarkerFile != "" && b.markerOn(dir) {
			jar, err := b.markerJar(side, cond, sel)
			if err != nil {
				return nil, nil, err
			}
			desired[markerJarPath(b.Manifest.Name)] = source{content: literal{jar}}
		}
	}
	whole := func(rel string) bool {
		for _, pattern := range b.Manifest.WholeFiles {
			if ok, _ := path.Match(pattern, rel); ok {
				return true
			}
		}
		return false
	}
	for _, l := range b.overrideLayers(side, cond, vars) {
		if err := b.layer(l, whole, desired, report); err != nil {
			return nil, nil, err
		}
	}
	if side == "client" {
		if err := b.reportPackList(side, opts, desired, shipped, report); err != nil {
			return nil, nil, err
		}
	}
	return desired, dirs, nil
}

type overrideLayer struct {
	root    string
	label   string
	pack    string
	feature string
	vars    map[string]string
	skips   func(rel string) bool
	// archived marks a layer read from a modpack archive: files holds it, and root only names
	// where each file came from.
	archived bool
	files    []packarchive.Override
}

// overrideLayers is every override folder that applies to the side, in build
// order: each pulled pack's folders in requires order, then the project's, and
// within each the shared folder, the side's, then the enabled features in name
// order. A folder that doesn't exist is skipped when it is walked.
func (b *Builder) overrideLayers(side string, cond conditions, vars map[string]string) []overrideLayer {
	var layers []overrideLayer
	add := func(m *manifest.Manifest, dir, pack string, vars map[string]string) {
		label := func(folder string) string {
			if pack == "" {
				return folder
			}
			return pack + ":" + folder
		}
		for _, folder := range []string{"overrides", side + "-overrides"} {
			layers = append(layers, overrideLayer{root: filepath.Join(dir, folder), label: label(folder), pack: pack, vars: vars, skips: m.Skips})
		}
		for _, name := range slices.Sorted(maps.Keys(m.Features)) {
			if !cond.features[name] {
				continue
			}
			for _, folder := range featureFolders(name, m.Features[name], side) {
				layers = append(layers, overrideLayer{root: filepath.Join(dir, folder), label: label(folder), pack: pack, feature: name, vars: vars, skips: m.Skips})
			}
		}
	}
	for _, pk := range b.Packs {
		packVars := pulledTemplateVars(pk.Manifest, b.Lock, side, vars)
		if pk.Archive != nil {
			for _, folder := range []string{"overrides", side + "-overrides"} {
				l := overrideLayer{root: filepath.Join(b.Dir, filepath.FromSlash(pk.Source), folder), label: pk.Name + ":" + folder, pack: pk.Name, vars: packVars, skips: pk.Manifest.Skips, archived: true}
				for _, o := range pk.Overrides {
					if o.Layer == folder && !l.skips(o.Path) {
						l.files = append(l.files, o)
					}
				}
				layers = append(layers, l)
			}
			continue
		}
		if pk.Dir == "" {
			continue
		}
		add(pk.Manifest, pk.Dir, pk.Name, packVars)
	}
	add(b.Manifest, b.Dir, "", vars)
	return layers
}

// OverrideFile is what the side's build would place at rel from its override folders, with the
// default features on; ok is false when no folder places it. A .properties file comes back as the
// last folder's copy, not merged key by key as the build merges it.
func (b *Builder) OverrideFile(side, rel string) (data []byte, ok bool, err error) {
	desired := map[string]source{}
	for _, l := range b.overrideLayers(side, b.conditions(Options{}), templateVars(b.Manifest, b.Lock, side)) {
		skips := l.skips
		l.skips = func(p string) bool { return (p != rel && p != rel+TemplateSuffix) || skips(p) }
		if err := b.layer(l, func(string) bool { return true }, desired, &Report{}); err != nil {
			return nil, false, err
		}
	}
	s, ok := desired[rel]
	return s.data(), ok, nil
}

func featureFolders(name string, f manifest.Feature, side string) []string {
	var folders []string
	switch {
	case f.Overrides.Both != "":
		folders = append(folders, f.Overrides.Both)
	case f.Overrides.Client == "" && f.Overrides.Server == "":
		folders = append(folders, name+"-overrides")
	}
	if own := f.Overrides.For(side); own != "" {
		folders = append(folders, own)
	}
	return folders
}

func (b *Builder) layer(l overrideLayer, whole func(string) bool, desired map[string]source, report *Report) error {
	root := l.root
	if l.archived {
		for _, o := range l.files {
			if l.skips(o.Path) {
				continue
			}
			if err := b.layFile(l, filepath.Join(root, filepath.FromSlash(o.Path)), o.Path, o.Data, whole, desired, report); err != nil {
				return err
			}
		}
		return nil
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == root {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if l.skips(filepath.ToSlash(rel)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return b.layFile(l, path, filepath.ToSlash(rel), data, whole, desired, report)
	})
}

// layFile puts one override file, read from path, at rel in desired.
func (b *Builder) layFile(l overrideLayer, path, rel string, data []byte, whole func(string) bool, desired map[string]source, report *Report) error {
	var err error
	src := source{origin: path, pack: l.pack, feature: l.feature}
	if strings.HasSuffix(rel, TemplateSuffix) {
		rel = strings.TrimSuffix(rel, TemplateSuffix)
		src.isTemplate = true
		if data, err = render(l.label+"/"+rel+TemplateSuffix, l.pack, data, l.vars); err != nil {
			return err
		}
	}
	if strings.HasSuffix(rel, ".properties") && !whole(rel) {
		desired[rel] = mergedProperties(desired[rel], data, keySource{path: path, pack: l.pack, feature: l.feature, isTemplate: src.isTemplate}, src, rel, report)
		return nil
	}
	if prev, taken := desired[rel]; taken {
		warnFeatureConflict(report, prev.feature, l.feature, rel)
	}
	if under := desired[rel].owned(); under != nil {
		src.managed = under
		if data, err = under.render(data, nil, nil); err != nil {
			return err
		}
	}
	src.content = literal{data}
	desired[rel] = src
	return nil
}

func (b *Builder) collectServer(desired map[string]source, vars map[string]string, cond conditions, noLauncher, eula bool, report *Report) (string, error) {
	if !noLauncher {
		if err := b.collectLauncher(desired); err != nil {
			return "", err
		}
	}
	srv := b.Manifest.Server
	if srv == nil {
		srv = &manifest.Server{}
	}
	if eula {
		desired[EulaFile] = source{content: literal{[]byte("eula=true\n")}}
	}
	props, err := renderProperties(PropertiesFile, srv.Properties, vars)
	if err != nil {
		return "", err
	}
	if srv.ResourcePack != "" {
		if err := b.pushResourcePack(srv.ResourcePack, cond, props, report); err != nil {
			return "", err
		}
	}
	if err := b.checkProperties(props, report); err != nil {
		return "", err
	}
	desired[PropertiesFile] = ownedSource(propsFile{props: props, sep: "="})
	if err := b.collectPlayers(srv.Players, desired); err != nil {
		return "", err
	}
	return levelName(props), nil
}

func levelName(props properties) string {
	if name := props["level-name"]; name != "" {
		return name
	}
	return "world"
}

func (b *Builder) collectLauncher(desired map[string]source) error {
	launcherMissing := notInstalled("the server launcher")
	l := loader.Running(b.Lock)
	jar, vanilla := b.Lock.Loader.Server, b.Lock.Server
	if vanilla == nil || !b.Cache.Has(vanilla.Sha512) {
		return launcherMissing
	}
	desired[l.VanillaServerPath(b.Lock.Minecraft)] = fromCache(vanilla.Sha512)
	if b.Lock.Loader.Type == "" {
		return nil
	}
	if jar == nil || !b.Cache.Has(jar.Sha512) {
		return launcherMissing
	}
	if l.ServerLaunchJar != "" {
		desired[l.ServerLaunchJar] = fromCache(jar.Sha512)
	}
	for name, dl := range jar.Libraries {
		path, err := loader.MavenPath(name)
		if err != nil {
			return err
		}
		if !b.Cache.Has(dl.Sha512) {
			return launcherMissing
		}
		desired["libraries/"+path] = fromCache(dl.Sha512)
	}
	return nil
}

// LaunchArgs start the server from its dir, as the locked loader has it.
func LaunchArgs(lk *lock.Lock) []string {
	return loader.Running(lk).LaunchArgs(lk)
}

// RecordLoader notes in dir's state that l was set up by its own installer.
func RecordLoader(dir string, l InstalledLoader) error {
	s := LoadState(dir)
	s.InstalledLoader = &l
	return writeState(dir, s)
}

// RecordLauncherImage notes in dir's state the hash of the instance image written into the launcher.
func RecordLauncherImage(dir, hash string) error {
	s := LoadState(dir)
	s.LauncherImage = hash
	return writeState(dir, s)
}

func (b *Builder) collectClient(side string, opts Options, desired map[string]source, vars map[string]string, shipped string, report *Report) error {
	cl := b.Manifest.Client
	file := b.Manifest.OptionsPath()
	options := properties{}
	if cl != nil && len(cl.Options) > 0 {
		rendered, err := renderProperties(file, cl.Options, vars)
		if err != nil {
			return err
		}
		options = rendered
	}
	if err := b.seedResourcePacks(side, opts, desired, options, shipped, report); err != nil {
		return err
	}
	if len(options) == 0 {
		return nil
	}
	desired[file] = ownedSource(propsFile{props: options, sep: ":"})
	return nil
}

func (b *Builder) checkProperties(props properties, report *Report) error {
	mc, err := minecraft.Parse(b.Lock.Minecraft)
	if err != nil {
		return out.Errorf("properties-invalid", "server.properties can't be checked against Minecraft %s", b.Lock.Minecraft).WithCause("minecraft", err)
	}
	check := server.CheckProperties(props, mc)
	report.Warnings = append(report.Warnings, check.Warnings...)
	if len(check.Problems) > 0 {
		e := out.Errorf("properties-invalid", "%d server.properties key(s) are not valid for Minecraft %s", len(check.Problems), b.Lock.Minecraft)
		e.Items = check.Problems
		return e
	}
	return nil
}

func renderProperties(file string, raw map[string]any, vars map[string]string) (properties, error) {
	props := properties{}
	for key, v := range raw {
		value, err := formatProperty(v)
		if err != nil {
			return nil, out.Errorf("properties-invalid", "shulker.json %s key %s has a value shulker can't write", file, key).WithCause("value", err)
		}
		rendered, err := render("shulker.json "+file+" "+key, "", []byte(value), vars)
		if err != nil {
			return nil, err
		}
		props[key] = string(rendered)
	}
	return props, nil
}

func (b *Builder) hashSource(s source) (string, error) { return s.content.hash(b) }

func (b *Builder) plan(dir string, desired map[string]source, prev State, force bool) ([]planned, error) {
	paths := make([]string, 0, len(desired))
	for p := range desired {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var plans []planned
	for _, rel := range paths {
		src := desired[rel]
		newHash, err := b.hashSource(src)
		if err != nil {
			return nil, err
		}
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		f := planned{rel: rel, src: src, hash: newHash}
		if src.owned() != nil {
			existing, err := os.ReadFile(abs)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			f.merge = mergeKeys(src.owned(), existing, prev.Values[rel], prev.recordedKeys(rel), force)
			f.isForced = f.merge.isForced
			switch {
			case f.merge.hasChanged:
				f.state = stateWrite
			case len(f.merge.kept) > 0:
				f.state = stateKept
			default:
				f.state = stateUnchanged
			}
			plans = append(plans, f)
			continue
		}
		current, exists, err := fileSha256(abs)
		if err != nil {
			return nil, err
		}
		recorded := prev.Files[rel]
		untouched := false
		if exists {
			if untouched, err = prev.isUntouched(rel, abs, current); err != nil {
				return nil, err
			}
		}
		switch {
		case !exists:
			f.state = stateWrite
		case current == newHash:
			f.state = stateUnchanged
		case recorded == "" && !force:
			f.state = stateUntracked
		case untouched:
			f.state = stateWrite
		case force:
			f.state = stateWrite
			f.isForced = true
		case newHash == recorded:
			f.state = stateKept
		default:
			f.state = stateConflict
		}
		plans = append(plans, f)
	}
	stale := make([]string, 0, len(prev.Files))
	for rel := range prev.Files {
		if _, still := desired[rel]; !still {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	for _, rel := range stale {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		current, exists, err := fileSha256(abs)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		untouched, err := prev.isUntouched(rel, abs, current)
		if err != nil {
			return nil, err
		}
		state := stateRemove
		edited := !untouched
		if edited && !force {
			state = stateOrphan
		}
		plans = append(plans, planned{rel: rel, state: state, isForced: edited && force})
	}
	return plans, nil
}

func (b *Builder) write(abs string, s source, m keyMerge) error {
	if c, ok := s.content.(cached); ok {
		return b.Cache.CopyTo(c.sha512, abs)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	data, err := b.output(abs, s, m)
	if err != nil {
		return err
	}
	return fsutil.Write(abs, data)
}

func (b *Builder) output(abs string, s source, m keyMerge) ([]byte, error) {
	return s.content.bytes(b, abs, m)
}

func canonicalValues(values map[string]string) []byte {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	for _, k := range keys {
		fmt.Fprintf(&buf, "%s=%s\n", k, values[k])
	}
	return buf.Bytes()
}

// HasEula is whether dir holds an eula.txt, however it got there; the server reads it as it is.
func HasEula(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, EulaFile))
	return err == nil
}

// handwritten is whether rel is in dir without shulker having written it there.
func handwritten(dir, rel string) bool {
	if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
		return false
	}
	_, ours := LoadState(dir).Files[rel]
	return !ours
}

// LoadState is ReadState without the reason: a state it can't read is empty.
func LoadState(dir string) State {
	s, _ := ReadState(dir)
	return s
}

// StateError is why ReadState treated a state file as empty: every file in the directory then counts
// as not written by shulker.
type StateError struct {
	Path string
	// Newer is a file written by a newer shulker, whose fix is `shulker self update` rather than
	// --force.
	Newer bool
	// Cause is why, without the path.
	Cause error
}

func (e *StateError) Error() string {
	if e.Newer {
		return fmt.Sprintf("%s %v; treating every file as not written by shulker", e.Path, e.Cause)
	}
	return fmt.Sprintf("%s is unreadable (%v); treating every file as not written by shulker", e.Path, e.Cause)
}

// ReadState treats a state file it can't read as empty, like LoadState, and also returns why.
func ReadState(dir string) (State, *StateError) {
	path := StatePath(dir)
	empty := State{Files: map[string]string{}}
	var s State
	err := managed.Read(schema.State, path, &s)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		e := out.AsError(err)
		if e.Cause == nil {
			return empty, &StateError{Path: path, Cause: err}
		}
		return empty, &StateError{Path: path, Newer: e.Code == "schema-newer", Cause: e.Cause}
	}
	if s.Files == nil {
		s.Files = map[string]string{}
	}
	return s, nil
}

func (b *Builder) saveState(dir string, s State) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeState(dir, s)
}

func sameDir(a, b string) bool {
	x, errA := filepath.Abs(a)
	y, errB := filepath.Abs(b)
	return errA == nil && errB == nil && x == y
}

// checkReserved keeps a build in place off the files the project and the player
// own: an override layer can write anything except these.
func checkReserved(side string, desired map[string]source, data []string) error {
	var bad []string
	for rel := range desired {
		// A server's world is data, but its datapacks folder is the build's to fill.
		if side == "server" && path.Dir(rel) == data[0]+"/datapacks" {
			continue
		}
		if reservedPath(rel, data) {
			bad = append(bad, rel)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	noun := "files"
	if len(bad) == 1 {
		noun = "file"
	}
	e := out.Errorf("build-reserved", "%s builds in place, so it can't write %d %s it doesn't own", side, len(bad), noun)
	e.Items = bad
	return e
}

func reservedPath(rel string, data []string) bool {
	switch rel {
	case manifest.FileName, lock.FileName, local.FileName:
		return true
	}
	top, _, _ := strings.Cut(rel, "/")
	return top == StateDir || top == DataDir || top == manifest.FilesDir || slices.Contains(data, top)
}

func writeState(dir string, s State) error {
	if err := os.MkdirAll(filepath.Join(dir, StateDir), 0o755); err != nil {
		return err
	}
	s.Schema = schema.URL(schema.State)
	return fsutil.WriteJSON(StatePath(dir), s)
}

func fileSha256(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return sha256Hex(data), true, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r *Report) Summary() string {
	s := fmt.Sprintf("%s: %d written, %d unchanged, %d kept, %d removed", r.Side, len(r.Written), r.Unchanged, len(r.Kept), len(r.Removed))
	if len(r.Linked) > 0 {
		s += fmt.Sprintf(", %d linked", len(r.Linked))
	}
	if len(r.Moved) > 0 {
		s += fmt.Sprintf(", %d moved", len(r.Moved))
	}
	if len(r.Excluded) > 0 {
		s += fmt.Sprintf(", %d excluded", len(r.Excluded))
	}
	return s
}

func notInstalled(what string) *out.Error {
	e := out.Errorf("not-installed", "%s is not in the cache", what)
	e.Help = "run `shulker install`"
	return e
}
