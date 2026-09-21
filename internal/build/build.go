// Package build writes a project's side into a directory: mods, packs, overrides and rendered
// config, tracking what it wrote so a rebuild can tell its own files from the player's edits.
package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mcver"
	"shulker.sh/shulker/internal/meta"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/server"
)

const (
	StateDir          = ".shulker"
	StateFile         = "state.json"
	TemplateSuffix    = ".tmpl"
	VanillaServerFile = "server.jar"
	EulaFile          = "eula.txt"
)

func StatePath(dir string) string {
	return filepath.Join(dir, StateDir, StateFile)
}

// Origin is where the project a build came from was synced from: a source, a ref and commit, or
// an archive's sha256. It is empty for a build of the local project.
type Origin struct {
	Source string `json:"source,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Commit string `json:"commit,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// State is .shulker/state.json: what the last build wrote into a directory, and the hash of each
// file it owns.
type State struct {
	Side string `json:"side"`
	Origin
	BuiltAt       string                       `json:"builtAt"`
	LockSha256    string                       `json:"lockSha256"`
	Minecraft     string                       `json:"minecraft,omitempty"`
	Loader        string                       `json:"loader,omitempty"`
	LoaderVersion string                       `json:"loaderVersion,omitempty"`
	Files         map[string]string            `json:"files"`
	Values        map[string]map[string]string `json:"managedValues,omitempty"`
	Links         []string                     `json:"links,omitempty"`
	// InstalledLoader is the loader its own installer set up in the dir; the installer's files aren't tracked.
	InstalledLoader *InstalledLoader `json:"installedLoader,omitempty"`
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
	Excluded  []string `json:"excluded"`
	Warnings  []string `json:"-"`
	Forced    bool     `json:"forced"`
	History   string   `json:"history,omitempty"`
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
	// BeforeModChange runs once, before a build that adds, replaces or removes a mod writes
	// anything.
	BeforeModChange func() error
}

// Builder builds, diffs and exports one project from its manifest, lock and cached files.
type Builder struct {
	Dir      string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
	LockPath string
	Cache    *cache.Cache
	Packs    []*pack.Loaded
}

type source struct {
	sha512     string
	data       []byte
	owned      ownedFile
	origin     string
	pack       string
	feature    string
	isTemplate bool
	managed    ownedFile
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
	if stateErr != nil {
		report.Warnings = append(report.Warnings, stateErr.Error())
	}
	next := State{Side: side, Origin: opts.Origin, Files: map[string]string{}, InstalledLoader: prev.InstalledLoader}
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
			if f.src.owned == nil {
				report.Kept = append(report.Kept, f.rel)
			}
		case stateConflict:
			report.Conflicts = append(report.Conflicts, f.rel+" (changed in place and in the source)")
			continue
		case stateUntracked:
			report.Conflicts = append(report.Conflicts, f.rel+" (not written by shulker)")
			continue
		case stateOrphan:
			report.Kept = append(report.Kept, f.rel+" (edited; no longer in source)")
			continue
		case stateRemove:
			report.Removed = append(report.Removed, f.rel)
			continue
		}
		next.Files[f.rel] = f.hash
		if f.src.owned != nil {
			next.record(f.rel, f.src.owned)
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
	for _, rel := range writes {
		if err := b.write(filepath.Join(dir, filepath.FromSlash(rel)), desired[rel], merges[rel]); err != nil {
			return nil, err
		}
		report.Written = append(report.Written, rel)
	}
	for _, rel := range report.Removed {
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
	if err := b.applyLinks(dir, side, links, report); err != nil {
		return nil, err
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
	for id, m := range b.Lock.Mods {
		if !sel.included[id] || (m.Side != "both" && m.Side != side) {
			continue
		}
		if !b.Cache.Has(m.Sha512) {
			return nil, nil, notInstalled(id)
		}
		desired["mods/"+m.Filename] = source{sha512: m.Sha512}
	}
	vars := b.Manifest.SideVariables(side).Text()
	if side == "server" {
		levelName, err := b.collectServer(desired, vars, report)
		if err != nil {
			return nil, nil, err
		}
		dirs = dataDirs(side, levelName)
	}
	if side == "client" {
		if err := b.collectPacks(cond, desired, report); err != nil {
			return nil, nil, err
		}
		b.enableShader(desired)
		if err := b.collectClient(side, opts, desired, vars, report); err != nil {
			return nil, nil, err
		}
		if b.Lock.Loader.Type != "" && b.markerOn(dir) {
			jar, err := b.markerJar(side, cond, sel)
			if err != nil {
				return nil, nil, err
			}
			desired[markerJarPath(b.Manifest.Name)] = source{data: jar}
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
	return desired, dirs, nil
}

type overrideLayer struct {
	root    string
	label   string
	pack    string
	feature string
	vars    map[string]string
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
			layers = append(layers, overrideLayer{root: filepath.Join(dir, folder), label: label(folder), pack: pack, vars: vars})
		}
		for _, name := range slices.Sorted(maps.Keys(m.Features)) {
			if !cond.features[name] {
				continue
			}
			for _, folder := range featureFolders(name, m.Features[name], side) {
				layers = append(layers, overrideLayer{root: filepath.Join(dir, folder), label: label(folder), pack: pack, feature: name, vars: vars})
			}
		}
	}
	for _, pk := range b.Packs {
		if pk.Dir == "" {
			continue
		}
		packVars := map[string]string{}
		maps.Copy(packVars, pk.Manifest.SideVariables(side).Text())
		maps.Copy(packVars, vars)
		add(pk.Manifest, pk.Dir, pk.Name, packVars)
	}
	add(b.Manifest, b.Dir, "", vars)
	return layers
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
	root, label, pack, vars := l.root, l.label, l.pack, l.vars
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
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := source{origin: path, pack: pack, feature: l.feature}
		if strings.HasSuffix(rel, TemplateSuffix) {
			rel = strings.TrimSuffix(rel, TemplateSuffix)
			src.isTemplate = true
			if data, err = render(label+"/"+rel+TemplateSuffix, data, vars); err != nil {
				return err
			}
		}
		if strings.HasSuffix(rel, ".properties") && !whole(rel) {
			desired[rel] = mergedProperties(desired[rel], data, keySource{path: path, pack: pack, feature: l.feature, isTemplate: src.isTemplate}, src, rel, report)
			return nil
		}
		if prev, taken := desired[rel]; taken {
			warnFeatureConflict(report, prev.feature, l.feature, rel)
		}
		if owned := desired[rel].owned; owned != nil {
			src.managed = owned
			if data, err = owned.render(data, nil, nil); err != nil {
				return err
			}
		}
		src.data = data
		desired[rel] = src
		return nil
	})
}

func (b *Builder) collectServer(desired map[string]source, vars map[string]string, report *Report) (string, error) {
	if err := b.collectLauncher(desired); err != nil {
		return "", err
	}
	srv := b.Manifest.Server
	if srv == nil {
		srv = &manifest.Server{}
	}
	if srv.Eula {
		desired[EulaFile] = source{data: []byte("eula=true\n")}
	}
	props, err := renderProperties(PropertiesFile, srv.Properties, vars)
	if err != nil {
		return "", err
	}
	if err := b.checkProperties(props, report); err != nil {
		return "", err
	}
	desired[PropertiesFile] = source{owned: propsFile{props: props, sep: "="}}
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
	l, _ := loader.Lookup(b.Lock.Loader.Type)
	jar, vanilla := b.Lock.Loader.Server, b.Lock.Server
	if vanilla == nil || !b.Cache.Has(vanilla.Sha512) {
		return launcherMissing
	}
	desired[vanillaServerPath(l, b.Lock.Minecraft)] = source{sha512: vanilla.Sha512}
	if b.Lock.Loader.Type == "" {
		return nil
	}
	if jar == nil || !b.Cache.Has(jar.Sha512) {
		return launcherMissing
	}
	if l.InstallServerFlag == "" {
		desired[l.ServerLaunchJar] = source{sha512: jar.Sha512}
	}
	for name, dl := range jar.Libraries {
		path, err := meta.MavenPath(name)
		if err != nil {
			return err
		}
		if !b.Cache.Has(dl.Sha512) {
			return launcherMissing
		}
		desired["libraries/"+path] = source{sha512: dl.Sha512}
	}
	return nil
}

// vanillaServerPath is where a loader looks for the vanilla server jar: Fabric's launcher in its data
// dir, where it downloads the jar only when missing; Quilt's launcher next to itself, which is also
// where a project without a loader runs it; NeoForge's and Forge's installers under libraries/.
func vanillaServerPath(l loader.Loader, minecraft string) string {
	switch {
	case l.InstallServerFlag != "":
		name := "server-" + minecraft
		if l.MinecraftJarClassifier != "" {
			name += "-" + l.MinecraftJarClassifier
		}
		return "libraries/net/minecraft/server/" + minecraft + "/" + name + ".jar"
	case l.Name == "fabric":
		return ".fabric/server/" + minecraft + "-server.jar"
	}
	return VanillaServerFile
}

// LaunchArgs start the server from its dir: the launch jar, the args file a loader's installer
// wrote, or the vanilla jar when there is no loader.
func LaunchArgs(lk *lock.Lock) []string {
	if file := InstallerArgsFile(lk); file != "" {
		return []string{"@" + file}
	}
	info, ok := loader.Lookup(lk.Loader.Type)
	if !ok {
		return []string{"-jar", VanillaServerFile}
	}
	return []string{"-jar", info.ServerLaunchJar}
}

// InstallerArgsFile is the args file a loader's server installer writes, relative to the server
// directory, or empty when the loader has no installer.
func InstallerArgsFile(lk *lock.Lock) string {
	l, _ := loader.Lookup(lk.Loader.Type)
	if l.InstallServerFlag == "" {
		return ""
	}
	name := "unix_args.txt"
	if runtime.GOOS == "windows" {
		name = "win_args.txt"
	}
	return "libraries/" + l.MavenPath + "/" + l.ArtifactVersion(lk.Minecraft, lk.Loader.Version) + "/" + name
}

// RecordLoader notes in dir's state that l was set up by its own installer.
func RecordLoader(dir string, l InstalledLoader) error {
	s := LoadState(dir)
	s.InstalledLoader = &l
	return writeState(dir, s)
}

func (b *Builder) collectClient(side string, opts Options, desired map[string]source, vars map[string]string, report *Report) error {
	cl := b.Manifest.Client
	options := properties{}
	if cl != nil && len(cl.Options) > 0 {
		rendered, err := renderProperties(OptionsFile, cl.Options, vars)
		if err != nil {
			return err
		}
		options = rendered
	}
	b.seedResourcePacks(side, opts, desired, options, report)
	if len(options) == 0 {
		return nil
	}
	desired[OptionsFile] = source{owned: propsFile{props: options, sep: ":"}}
	return nil
}

func (b *Builder) checkProperties(props properties, report *Report) error {
	minecraft, err := mcver.Parse(b.Lock.Minecraft)
	if err != nil {
		e := out.Errorf("properties-invalid", "server.properties can't be checked against Minecraft %s", b.Lock.Minecraft)
		e.Rows = []out.Detail{{Label: "minecraft", Text: err.Error()}}
		return e
	}
	check := server.CheckProperties(props, minecraft)
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
			e := out.Errorf("properties-invalid", "shulker.json %s key %s has a value shulker can't write", file, key)
			e.Rows = []out.Detail{{Label: "value", Text: err.Error()}}
			return nil, e
		}
		rendered, err := render("shulker.json "+file+" "+key, []byte(value), vars)
		if err != nil {
			return nil, err
		}
		props[key] = string(rendered)
	}
	return props, nil
}

func (b *Builder) hashSource(s source) (string, error) {
	if s.owned != nil {
		return sha256Hex(canonicalValues(s.owned.values())), nil
	}
	if s.sha512 != "" {
		data, err := os.ReadFile(b.Cache.Object(s.sha512))
		if err != nil {
			return "", err
		}
		return sha256Hex(data), nil
	}
	return sha256Hex(s.data), nil
}

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
		if src.owned != nil {
			existing, err := os.ReadFile(abs)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			f.merge = mergeKeys(src.owned, existing, prev.Values[rel], prev.recordedKeys(rel), force)
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
		switch {
		case !exists:
			f.state = stateWrite
		case current == newHash:
			f.state = stateUnchanged
		case recorded == "" && !force:
			f.state = stateUntracked
		case current == recorded:
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
		state := stateRemove
		edited := current != prev.Files[rel]
		if edited && !force {
			state = stateOrphan
		}
		plans = append(plans, planned{rel: rel, state: state, isForced: edited && force})
	}
	return plans, nil
}

func (b *Builder) write(abs string, s source, m keyMerge) error {
	if s.sha512 != "" {
		return b.Cache.CopyTo(s.sha512, abs)
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
	if s.sha512 != "" {
		return os.ReadFile(b.Cache.Object(s.sha512))
	}
	if s.owned == nil {
		return s.data, nil
	}
	existing, err := os.ReadFile(abs)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return s.owned.render(existing, m.kept, m.dropped)
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

// LoadState is ReadState without the reason: a state it can't read is empty.
func LoadState(dir string) State {
	s, _ := ReadState(dir)
	return s
}

// ReadState treats a state file it can't read as empty, like LoadState, and also returns why,
// worded as the warning to show.
func ReadState(dir string) (State, error) {
	path := StatePath(dir)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{Files: map[string]string{}}, nil
	}
	var s State
	if err == nil {
		err = json.Unmarshal(data, &s)
	}
	if err != nil {
		return State{Files: map[string]string{}}, fmt.Errorf("%s is unreadable (%v); treating every file as not written by shulker. Rebuild with --force to take them over", path, err)
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
	return top == StateDir || top == DataDir || slices.Contains(data, top)
}

func writeState(dir string, s State) error {
	if err := os.MkdirAll(filepath.Join(dir, StateDir), 0o755); err != nil {
		return err
	}
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
