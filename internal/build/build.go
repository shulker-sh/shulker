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

	"github.com/andrewmast/shulker/internal/cache"
	"github.com/andrewmast/shulker/internal/lock"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
)

const (
	StateFile      = ".shulker-state.json"
	TemplateSuffix = ".tmpl"
	ServerJarFile  = "fabric-server-launch.jar"
	EulaFile       = "eula.txt"
)

type State struct {
	Target     string            `json:"target"`
	BuiltAt    string            `json:"builtAt"`
	LockSha256 string            `json:"lockSha256"`
	Files      map[string]string `json:"files"`
}

type Report struct {
	Target    string   `json:"target"`
	Dir       string   `json:"dir"`
	Written   []string `json:"written"`
	Unchanged int      `json:"unchanged"`
	Kept      []string `json:"kept"`
	Removed   []string `json:"removed"`
	Conflicts []string `json:"conflicts"`
	Forced    bool     `json:"forced"`
}

type Options struct {
	Force bool
}

type Builder struct {
	Dir      string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
	LockPath string
	Cache    *cache.Cache
}

type source struct {
	sha512 string
	data   []byte
	props  properties
}

func (b *Builder) Build(name string, opts Options) (*Report, error) {
	target, ok := b.Manifest.Targets[name]
	if !ok {
		return nil, out.Errorf("unknown-target", "target %q is not in the manifest", name)
	}
	dir := filepath.Join(b.Dir, target.Build)
	desired, err := b.collect(target)
	if err != nil {
		return nil, err
	}
	prev := b.loadState(dir)
	report := &Report{Target: name, Dir: dir, Written: []string{}, Kept: []string{}, Removed: []string{}, Conflicts: []string{}, Forced: opts.Force}
	next := State{Target: name, Files: map[string]string{}}

	paths := make([]string, 0, len(desired))
	for p := range desired {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var writes []string
	for _, rel := range paths {
		src := desired[rel]
		newHash, err := b.hashSource(src)
		if err != nil {
			return nil, err
		}
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		current, exists, err := b.currentHash(abs, src)
		if err != nil {
			return nil, err
		}
		recorded := prev.Files[rel]
		switch {
		case !exists:
			writes = append(writes, rel)
		case current == newHash:
			report.Unchanged++
		case recorded == "" && !opts.Force:
			report.Conflicts = append(report.Conflicts, rel+" (not written by shulker)")
			continue
		case current == recorded || opts.Force:
			writes = append(writes, rel)
		case newHash == recorded:
			report.Kept = append(report.Kept, rel)
		default:
			report.Conflicts = append(report.Conflicts, rel+" (changed in both build and source)")
			continue
		}
		next.Files[rel] = newHash
	}
	for rel, recorded := range prev.Files {
		if _, still := desired[rel]; still {
			continue
		}
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		current, exists, err := fileSha256(abs)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		if current != recorded && !opts.Force {
			report.Kept = append(report.Kept, rel+" (edited; no longer in source)")
			continue
		}
		report.Removed = append(report.Removed, rel)
	}
	if len(report.Conflicts) > 0 {
		e := out.Errorf("build-conflict", "%s: %d file(s) changed in the build directory and in the source; run `shulker diff`, or `build --force` to overwrite", name, len(report.Conflicts))
		e.Candidates = report.Conflicts
		return report, e
	}
	for _, rel := range writes {
		if err := b.write(filepath.Join(dir, filepath.FromSlash(rel)), desired[rel]); err != nil {
			return nil, err
		}
		report.Written = append(report.Written, rel)
	}
	for _, rel := range report.Removed {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
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

func (b *Builder) collect(target manifest.Target) (map[string]source, error) {
	desired := map[string]source{}
	for id, m := range b.Lock.Mods {
		if m.Side != "both" && m.Side != target.Side {
			continue
		}
		if !b.Cache.Has(m.Sha512) {
			return nil, out.Errorf("not-installed", "%s is not in the cache; run `shulker install`", id)
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
		if err := b.collectServer(desired, vars); err != nil {
			return nil, err
		}
	}
	if target.Side == "client" {
		jar, err := b.markerJar(target.Side)
		if err != nil {
			return nil, err
		}
		desired[markerJarPath(b.Manifest.Name)] = source{data: jar}
	}
	for _, layer := range target.Overrides {
		root := filepath.Join(b.Dir, layer)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
			if strings.HasSuffix(rel, TemplateSuffix) {
				rel = strings.TrimSuffix(rel, TemplateSuffix)
				if data, err = render(filepath.Join(layer, rel+TemplateSuffix), data, vars); err != nil {
					return err
				}
			}
			desired[rel] = source{data: data}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return desired, nil
}

func (b *Builder) collectServer(desired map[string]source, vars map[string]string) error {
	jar := b.Lock.Loader.Server
	if jar == nil || !b.Cache.Has(jar.Sha512) {
		return out.Errorf("not-installed", "the server launcher is not in the cache; run `shulker install`")
	}
	desired[ServerJarFile] = source{sha512: jar.Sha512}
	srv := b.Manifest.Server
	if srv == nil {
		return nil
	}
	if srv.Eula {
		desired[EulaFile] = source{data: []byte("eula=true\n")}
	}
	if len(srv.Properties) == 0 {
		return nil
	}
	props := properties{}
	for key, raw := range srv.Properties {
		value, err := formatProperty(raw)
		if err != nil {
			return fmt.Errorf("server.properties %s: %w", key, err)
		}
		rendered, err := render("shulker.json server.properties "+key, []byte(value), vars)
		if err != nil {
			return err
		}
		props[key] = string(rendered)
	}
	desired[PropertiesFile] = source{props: props}
	return nil
}

func (b *Builder) currentHash(abs string, s source) (string, bool, error) {
	if s.props == nil {
		return fileSha256(abs)
	}
	data, err := os.ReadFile(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return sha256Hex(s.props.restrict(parseProperties(data)).canonical()), true, nil
}

func (b *Builder) hashSource(s source) (string, error) {
	if s.props != nil {
		return sha256Hex(s.props.canonical()), nil
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

func (b *Builder) write(abs string, s source) error {
	if s.sha512 != "" {
		return b.Cache.CopyTo(s.sha512, abs)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	data := s.data
	if s.props != nil {
		existing, err := os.ReadFile(abs)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		data = s.props.mergeInto(existing)
	}
	return os.WriteFile(abs, data, 0o644)
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
	return fmt.Sprintf("%s: %d written, %d unchanged, %d kept, %d removed", r.Target, len(r.Written), r.Unchanged, len(r.Kept), len(r.Removed))
}
