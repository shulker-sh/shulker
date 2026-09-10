package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shulker-sh/shulker/internal/cache"
	"github.com/shulker-sh/shulker/internal/lock"
	"github.com/shulker-sh/shulker/internal/manifest"
	"github.com/shulker-sh/shulker/internal/mcver"
	"github.com/shulker-sh/shulker/internal/out"
	"github.com/shulker-sh/shulker/internal/pack"
	"github.com/shulker-sh/shulker/internal/server"
)

const (
	StateFile      = ".shulker-state.json"
	TemplateSuffix = ".tmpl"
	ServerJarFile  = "fabric-server-launch.jar"
	EulaFile       = "eula.txt"
)

type State struct {
	Target     string              `json:"target"`
	BuiltAt    string              `json:"builtAt"`
	LockSha256 string              `json:"lockSha256"`
	Files      map[string]string   `json:"files"`
	Keys       map[string][]string `json:"propertyKeys,omitempty"`
	Links      []string            `json:"links,omitempty"`
}

type Report struct {
	Target    string   `json:"target"`
	Dir       string   `json:"dir"`
	Written   []string `json:"written"`
	Unchanged int      `json:"unchanged"`
	Kept      []string `json:"kept"`
	Removed   []string `json:"removed"`
	Linked    []string `json:"linked"`
	Moved     []string `json:"moved"`
	Conflicts []string `json:"conflicts"`
	Warnings  []string `json:"warnings"`
	Forced    bool     `json:"forced"`
}

type Options struct {
	Force       bool
	Dir         string
	NoDataLinks bool
}

type Builder struct {
	Dir      string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
	LockPath string
	Cache    *cache.Cache
	Packs    []*pack.Loaded
}

type source struct {
	sha512   string
	data     []byte
	owned    ownedFile
	origin   string
	pack     string
	template bool
	managed  ownedFile
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
}

type ownedFile interface {
	keys() []string
	canonical() []byte
	current(existing []byte, recordedKeys []string) []byte
	merge(existing []byte, recordedKeys []string) ([]byte, error)
}

func (b *Builder) Build(name string, opts Options) (*Report, error) {
	target, ok := b.Manifest.Targets[name]
	if !ok {
		return nil, out.Errorf("unknown-target", "target %q is not in the manifest", name)
	}
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Join(b.Dir, target.Build)
	}
	report := &Report{Target: name, Dir: dir, Written: []string{}, Kept: []string{}, Removed: []string{}, Linked: []string{}, Moved: []string{}, Conflicts: []string{}, Warnings: []string{}, Forced: opts.Force}
	desired, dirs, err := b.collect(name, target, report)
	if err != nil {
		return nil, err
	}
	if opts.NoDataLinks {
		dirs = nil
	}
	prev := b.loadState(dir)
	next := State{Target: name, Files: map[string]string{}}
	links, err := b.planLinks(dir, name, dirs, prev, report)
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
			report.Kept = append(report.Kept, f.rel)
		case stateConflict:
			report.Conflicts = append(report.Conflicts, f.rel+" (changed in both build and source)")
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
			if next.Keys == nil {
				next.Keys = map[string][]string{}
			}
			next.Keys[f.rel] = f.src.owned.keys()
		}
	}
	if len(report.Conflicts) > 0 {
		e := out.Errorf("build-conflict", "%s: %d file(s) changed in the build directory and in the source; run `shulker diff`, or `build --force` to overwrite", name, len(report.Conflicts))
		e.Candidates = report.Conflicts
		return report, e
	}
	for _, rel := range writes {
		if err := b.write(filepath.Join(dir, filepath.FromSlash(rel)), desired[rel], prev.Keys[rel]); err != nil {
			return nil, err
		}
		report.Written = append(report.Written, rel)
	}
	for _, rel := range report.Removed {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	if err := b.applyLinks(dir, name, links, report); err != nil {
		return nil, err
	}
	next.BuiltAt = time.Now().UTC().Format(time.RFC3339)
	if next.LockSha256, err = lock.FileSha256(b.LockPath); err != nil {
		return nil, err
	}
	if err := b.saveState(dir, next); err != nil {
		return nil, err
	}
	return report, nil
}

func (b *Builder) collect(name string, target manifest.Target, report *Report) (map[string]source, []string, error) {
	desired := map[string]source{}
	dirs := dataDirs(target.Side, "world")
	for id, m := range b.Lock.Mods {
		if m.Side != "both" && m.Side != target.Side {
			continue
		}
		if !b.Cache.Has(m.Sha512) {
			return nil, nil, out.Errorf("not-installed", "%s is not in the cache; run `shulker install`", id)
		}
		desired["mods/"+m.Filename] = source{sha512: m.Sha512}
	}
	vars := map[string]string{}
	for k, v := range b.Manifest.Variables {
		vars[k] = v
	}
	for k, v := range target.Variables {
		vars[k] = v
	}
	if target.Side == "server" {
		levelName, err := b.collectServer(desired, vars, report)
		if err != nil {
			return nil, nil, err
		}
		dirs = dataDirs(target.Side, levelName)
	}
	if target.Side == "client" {
		if err := b.collectClient(desired, vars); err != nil {
			return nil, nil, err
		}
		jar, err := b.markerJar(name, target.Side)
		if err != nil {
			return nil, nil, err
		}
		desired[markerJarPath(b.Manifest.Name)] = source{data: jar}
	}
	for _, pk := range b.Packs {
		pt, err := pk.Target(name, target.Side)
		if err != nil {
			return nil, nil, err
		}
		if pt == nil || pk.Dir == "" {
			continue
		}
		packVars := map[string]string{}
		for _, layer := range []map[string]string{pk.Manifest.Variables, pt.Variables, vars} {
			for k, v := range layer {
				packVars[k] = v
			}
		}
		for _, layer := range pt.Overrides {
			if err := b.layer(filepath.Join(pk.Dir, layer), pk.Name+":"+layer, pk.Name, packVars, desired); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, layer := range target.Overrides {
		if err := b.layer(filepath.Join(b.Dir, layer), layer, "", vars, desired); err != nil {
			return nil, nil, err
		}
	}
	return desired, dirs, nil
}

func (b *Builder) layer(root, label, pack string, vars map[string]string, desired map[string]source) error {
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
		src := source{origin: path, pack: pack}
		if strings.HasSuffix(rel, TemplateSuffix) {
			rel = strings.TrimSuffix(rel, TemplateSuffix)
			src.template = true
			if data, err = render(label+"/"+rel+TemplateSuffix, data, vars); err != nil {
				return err
			}
		}
		if owned := desired[rel].owned; owned != nil {
			src.managed = owned
			if data, err = owned.merge(data, nil); err != nil {
				return err
			}
		}
		src.data = data
		desired[rel] = src
		return nil
	})
}

func (b *Builder) collectServer(desired map[string]source, vars map[string]string, report *Report) (string, error) {
	jar := b.Lock.Loader.Server
	if jar == nil || !b.Cache.Has(jar.Sha512) {
		return "", out.Errorf("not-installed", "the server launcher is not in the cache; run `shulker install`")
	}
	desired[ServerJarFile] = source{sha512: jar.Sha512}
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
	desired[PropertiesFile] = source{owned: propsFile{props, "="}}
	if err := b.collectPlayers(srv.Players, desired); err != nil {
		return "", err
	}
	levelName := props["level-name"]
	if levelName == "" {
		levelName = "world"
	}
	return levelName, nil
}

func (b *Builder) collectClient(desired map[string]source, vars map[string]string) error {
	cl := b.Manifest.Client
	if cl == nil || len(cl.Options) == 0 {
		return nil
	}
	options, err := renderProperties(OptionsFile, cl.Options, vars)
	if err != nil {
		return err
	}
	desired[OptionsFile] = source{owned: propsFile{options, ":"}}
	return nil
}

func (b *Builder) checkProperties(props properties, report *Report) error {
	minecraft, err := mcver.Parse(b.Lock.Minecraft)
	if err != nil {
		return err
	}
	check := server.CheckProperties(props, minecraft)
	report.Warnings = append(report.Warnings, check.Warnings...)
	if len(check.Problems) > 0 {
		e := out.Errorf("invalid-properties", "%d server.properties key(s) are not valid for Minecraft %s", len(check.Problems), b.Lock.Minecraft)
		e.Candidates = check.Problems
		return e
	}
	return nil
}

func renderProperties(file string, raw map[string]any, vars map[string]string) (properties, error) {
	props := properties{}
	for key, v := range raw {
		value, err := formatProperty(v)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", file, key, err)
		}
		rendered, err := render("shulker.json "+file+" "+key, []byte(value), vars)
		if err != nil {
			return nil, err
		}
		props[key] = string(rendered)
	}
	return props, nil
}

func (b *Builder) currentHash(abs string, s source, recordedKeys []string) (string, bool, error) {
	if s.owned == nil {
		return fileSha256(abs)
	}
	data, err := os.ReadFile(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return sha256Hex(s.owned.current(data, recordedKeys)), true, nil
}

func (b *Builder) hashSource(s source) (string, error) {
	if s.owned != nil {
		return sha256Hex(s.owned.canonical()), nil
	}
	if s.sha512 != "" {
		data, err := os.ReadFile(b.Cache.Path(s.sha512))
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
		current, exists, err := b.currentHash(abs, src, prev.Keys[rel])
		if err != nil {
			return nil, err
		}
		recorded := prev.Files[rel]
		f := planned{rel: rel, src: src, hash: newHash}
		switch {
		case !exists:
			f.state = stateWrite
		case current == newHash:
			f.state = stateUnchanged
		case recorded == "" && src.owned != nil:
			f.state = stateWrite
		case recorded == "" && !force:
			f.state = stateUntracked
		case current == recorded || force:
			f.state = stateWrite
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
		if current != prev.Files[rel] && !force {
			state = stateOrphan
		}
		plans = append(plans, planned{rel: rel, state: state})
	}
	return plans, nil
}

func (b *Builder) write(abs string, s source, recordedKeys []string) error {
	if s.sha512 != "" {
		return b.Cache.CopyTo(s.sha512, abs)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	data, err := b.output(abs, s, recordedKeys)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, data, 0o644)
}

func (b *Builder) output(abs string, s source, recordedKeys []string) ([]byte, error) {
	if s.sha512 != "" {
		return os.ReadFile(b.Cache.Path(s.sha512))
	}
	if s.owned == nil {
		return s.data, nil
	}
	existing, err := os.ReadFile(abs)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return s.owned.merge(existing, recordedKeys)
}

func (b *Builder) loadState(dir string) State {
	s := State{Files: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(dir, StateFile))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	if s.Files == nil {
		s.Files = map[string]string{}
	}
	return s
}

func (b *Builder) saveState(dir string, s State) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, StateFile), buf.Bytes(), 0o644)
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
	s := fmt.Sprintf("%s: %d written, %d unchanged, %d kept, %d removed", r.Target, len(r.Written), r.Unchanged, len(r.Kept), len(r.Removed))
	if len(r.Linked) > 0 {
		s += fmt.Sprintf(", %d linked", len(r.Linked))
	}
	if len(r.Moved) > 0 {
		s += fmt.Sprintf(", %d moved", len(r.Moved))
	}
	return s
}
