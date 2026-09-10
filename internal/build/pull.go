package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/shulker-sh/shulker/internal/out"
)

type FileDiff struct {
	Path  string `json:"path"`
	State string `json:"state"`
	Diff  string `json:"diff"`
}

type DiffReport struct {
	Target   string     `json:"target"`
	Dir      string     `json:"dir"`
	Files    []FileDiff `json:"files"`
	Warnings []string   `json:"warnings"`
}

type PullReport struct {
	Target          string   `json:"target"`
	Dir             string   `json:"dir"`
	Pulled          []string `json:"pulled"`
	Keys            []string `json:"keys"`
	Skipped         []string `json:"skipped"`
	Warnings        []string `json:"warnings"`
	ManifestChanged bool     `json:"manifestChanged"`
}

func (b *Builder) Diff(name string, opts Options) (*DiffReport, error) {
	d, err := b.drift(name, opts)
	if err != nil {
		return nil, err
	}
	report := &DiffReport{Target: name, Dir: d.dir, Files: []FileDiff{}, Warnings: d.warnings}
	for _, f := range d.plans {
		if !drifted(f.state) {
			continue
		}
		abs := filepath.Join(d.dir, filepath.FromSlash(f.rel))
		existing, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		var want []byte
		if f.state != stateOrphan {
			if want, err = b.output(abs, d.desired[f.rel], d.prev.Keys[f.rel]); err != nil {
				return nil, err
			}
		}
		report.Files = append(report.Files, FileDiff{Path: f.rel, State: string(f.state), Diff: unifiedDiff(f.rel, existing, want)})
	}
	return report, nil
}

func (b *Builder) Pull(name string, files []string, opts Options) (*PullReport, error) {
	d, err := b.drift(name, opts)
	if err != nil {
		return nil, err
	}
	dir, desired, prev, plans := d.dir, d.desired, d.prev, d.plans
	target := b.Manifest.Targets[name]
	report := &PullReport{Target: name, Dir: dir, Pulled: []string{}, Keys: []string{}, Skipped: []string{}, Warnings: d.warnings}
	named := map[string]bool{}
	for _, f := range files {
		named[filepath.ToSlash(filepath.Clean(f))] = true
	}
	states := map[string]fileState{}
	for _, f := range plans {
		states[f.rel] = f.state
	}
	for rel := range named {
		if !drifted(states[rel]) {
			e := out.Errorf("not-drifted", "%s is not changed in the build directory of %s", rel, name)
			e.Candidates = driftedPaths(plans)
			return nil, e
		}
	}
	var pulled []string
	for _, f := range plans {
		if !drifted(f.state) || (len(named) > 0 && !named[f.rel]) {
			continue
		}
		abs := filepath.Join(dir, filepath.FromSlash(f.rel))
		existing, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		src := desired[f.rel]
		var dest string
		switch {
		case f.state == stateUntracked || f.state == stateOrphan:
			if !named[f.rel] {
				reason := "not written by shulker"
				if f.state == stateOrphan {
					reason = "edited; no longer in source"
				}
				report.Skipped = append(report.Skipped, f.rel+" ("+reason+"; name it to adopt it)")
				continue
			}
			if len(target.Overrides) == 0 {
				return nil, out.Errorf("no-overrides", "target %s has no overrides directory to adopt %s into", name, f.rel)
			}
			dest = filepath.Join(b.Dir, target.Overrides[0], filepath.FromSlash(f.rel))
		case src.owned != nil:
			if _, ok := src.owned.(propsFile); !ok {
				report.Skipped = append(report.Skipped, f.rel+" (player files are managed by `shulker player`)")
				continue
			}
			keys := b.pullKeys(f.rel, src.owned.(propsFile), existing)
			report.Keys = append(report.Keys, keys...)
			if len(keys) > 0 {
				report.ManifestChanged = true
				pulled = append(pulled, f.rel)
			}
			continue
		case src.template:
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s (rendered from template %s)", f.rel, b.relPath(src.origin)))
			continue
		case src.pack != "":
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s (comes from pack %s)", f.rel, src.pack))
			continue
		case src.origin != "":
			dest = src.origin
		default:
			report.Skipped = append(report.Skipped, f.rel+" (managed by the lock)")
			continue
		}
		if src.managed != nil {
			keys := b.pullKeys(f.rel, src.managed.(propsFile), existing)
			report.Keys = append(report.Keys, keys...)
			if len(keys) > 0 {
				report.ManifestChanged = true
			}
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, existing, 0o644); err != nil {
			return nil, err
		}
		report.Pulled = append(report.Pulled, f.rel+" -> "+b.relPath(dest))
		pulled = append(pulled, f.rel)
	}
	if len(pulled) == 0 {
		return report, nil
	}
	desired, _, err = b.collect(name, target, &Report{})
	if err != nil {
		return nil, err
	}
	for _, rel := range pulled {
		src := desired[rel]
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		hash, _, err := b.currentHash(abs, src, nil)
		if err != nil {
			return nil, err
		}
		prev.Files[rel] = hash
		if src.owned != nil {
			if prev.Keys == nil {
				prev.Keys = map[string][]string{}
			}
			prev.Keys[rel] = src.owned.keys()
		}
	}
	if err := b.saveState(dir, prev); err != nil {
		return nil, err
	}
	return report, nil
}

type drift struct {
	dir      string
	desired  map[string]source
	prev     State
	plans    []planned
	warnings []string
}

func (b *Builder) drift(name string, opts Options) (*drift, error) {
	target, ok := b.Manifest.Targets[name]
	if !ok {
		return nil, out.Errorf("unknown-target", "target %q is not in the manifest", name)
	}
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Join(b.Dir, target.Build)
	}
	report := &Report{Warnings: []string{}}
	desired, _, err := b.collect(name, target, report)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, out.Errorf("not-built", "target %s has no build directory; run `shulker build`", name)
	}
	prev := b.loadState(dir)
	plans, err := b.plan(dir, desired, prev, false)
	if err != nil {
		return nil, err
	}
	return &drift{dir: dir, desired: desired, prev: prev, plans: plans, warnings: report.Warnings}, nil
}

func drifted(s fileState) bool {
	return s == stateKept || s == stateConflict || s == stateUntracked || s == stateOrphan
}

func driftedPaths(plans []planned) []string {
	var paths []string
	for _, f := range plans {
		if drifted(f.state) {
			paths = append(paths, f.rel)
		}
	}
	return paths
}

func (b *Builder) pullKeys(rel string, f propsFile, existing []byte) []string {
	raw := b.manifestBlock(rel)
	if raw == nil {
		return nil
	}
	current := parseProperties(existing)
	var changed []string
	for _, k := range f.props.keys() {
		value, ok := current[k]
		if !ok || value == f.props[k] {
			continue
		}
		raw[k] = typedProperty(raw[k], value)
		changed = append(changed, rel+" "+k+"="+value)
	}
	return changed
}

func (b *Builder) manifestBlock(rel string) map[string]any {
	switch rel {
	case PropertiesFile:
		if b.Manifest.Server != nil {
			return b.Manifest.Server.Properties
		}
	case OptionsFile:
		if b.Manifest.Client != nil {
			return b.Manifest.Client.Options
		}
	}
	return nil
}

func typedProperty(old any, value string) any {
	switch old.(type) {
	case bool:
		if v, err := strconv.ParseBool(value); err == nil {
			return v
		}
	case json.Number, float64:
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			return json.Number(value)
		}
	}
	return value
}

func (b *Builder) relPath(abs string) string {
	if rel, err := filepath.Rel(b.Dir, abs); err == nil {
		return filepath.ToSlash(rel)
	}
	return abs
}
