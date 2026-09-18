package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/near"
	"shulker.sh/shulker/internal/out"
)

type FileDiff struct {
	Path  string `json:"path"`
	State string `json:"state"`
	Diff  string `json:"diff"`
}

type DiffReport struct {
	Side     string     `json:"side"`
	Dir      string     `json:"dir"`
	Files    []FileDiff `json:"files"`
	Warnings []string   `json:"-"`
}

type PullReport struct {
	Side            string   `json:"side"`
	Dir             string   `json:"dir"`
	Pulled          []string `json:"pulled"`
	Keys            []string `json:"keys"`
	Adopted         []string `json:"adopted"`
	Skipped         []string `json:"skipped"`
	Warnings        []string `json:"-"`
	ManifestChanged bool     `json:"manifestChanged"`
}

func (b *Builder) Diff(side string, opts Options) (*DiffReport, error) {
	d, err := b.drift(side, opts)
	if err != nil {
		return nil, err
	}
	report := &DiffReport{Side: side, Dir: d.dir, Files: []FileDiff{}, Warnings: d.warnings}
	for _, f := range d.plans {
		if !drifted(f) {
			continue
		}
		abs := filepath.Join(d.dir, filepath.FromSlash(f.rel))
		existing, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		var project []byte
		state := f.state
		if f.state != stateOrphan {
			if project, err = b.output(abs, d.desired[f.rel], projectSide(f)); err != nil {
				return nil, err
			}
		}
		if f.src.owned != nil {
			state = stateKept
		}
		report.Files = append(report.Files, FileDiff{Path: f.rel, State: string(state), Diff: unifiedDiff(f.rel, project, existing)})
	}
	return report, nil
}

// projectSide renders what the project says for the keys edited on disk and leaves every other
// key as it is, so the diff of an owned file shows only the in-game edits.
func projectSide(f planned) keyMerge {
	if f.src.owned == nil {
		return keyMerge{}
	}
	m := keyMerge{kept: map[string]bool{}}
	for _, k := range f.src.owned.keys() {
		if !f.merge.kept[k] {
			m.kept[k] = true
		}
	}
	return m
}

// checkNamed runs before any build work, so a file name that isn't in the directory at all is
// reported as missing rather than as unchanged.
func (b *Builder) checkNamed(side string, files []string, opts Options) error {
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Join(b.Dir, b.Manifest.BuildDir(side))
	}
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	var present []string
	for _, f := range files {
		rel := filepath.ToSlash(filepath.Clean(f))
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if err == nil {
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if present == nil {
			if present, err = listFiles(dir); err != nil {
				return err
			}
		}
		msg := fmt.Sprintf("%s is not in %s", rel, dir)
		if hits := near.Closest(rel, present, 1); len(hits) > 0 {
			msg += fmt.Sprintf("; did you mean %s?", hits[0])
		}
		return out.Errorf("file-not-found", "%s", msg)
	}
	return nil
}

func listFiles(dir string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || e.Name() == StateFile {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files = append(files, filepath.ToSlash(rel))
		return err
	})
	return files, err
}

// Empty Files means every changed file; empty To keeps a file where it lives
// and puts a new one in overrides/.
type PullRequest struct {
	Files []string
	Keys  []string
	To    string
}

func (b *Builder) Pull(side string, req PullRequest, opts Options) (*PullReport, error) {
	files, adopt := req.Files, req.Keys
	if len(adopt) > 0 && len(files) != 1 {
		return nil, out.Errorf("usage", "--key needs exactly one file")
	}
	toDir, err := b.pullDest(side, req.To)
	if err != nil {
		return nil, err
	}
	if err := b.checkNamed(side, files, opts); err != nil {
		return nil, err
	}
	d, err := b.drift(side, opts)
	if err != nil {
		return nil, err
	}
	dir, desired, prev, plans := d.dir, d.desired, d.prev, d.plans
	report := &PullReport{Side: side, Dir: dir, Pulled: []string{}, Keys: []string{}, Adopted: []string{}, Skipped: []string{}, Warnings: d.warnings}
	var pulled []string
	if len(adopt) > 0 {
		rel := filepath.ToSlash(filepath.Clean(files[0]))
		if err := b.adoptKeys(rel, adopt, d, report); err != nil {
			return nil, err
		}
		files, plans, pulled = nil, nil, []string{rel}
	}
	named := map[string]bool{}
	for _, f := range files {
		named[filepath.ToSlash(filepath.Clean(f))] = true
	}
	byPath := map[string]planned{}
	for _, f := range plans {
		byPath[f.rel] = f
	}
	var fresh []string
	for _, rel := range slices.Sorted(maps.Keys(named)) {
		if f, ok := byPath[rel]; !ok {
			fresh = append(fresh, rel)
		} else if !drifted(f) {
			e := out.Errorf("not-drifted", "%s is not changed in the build directory of %s", rel, side)
			e.Candidates, e.Given = driftedPaths(plans), rel
			return nil, e
		}
	}
	for _, rel := range fresh {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		dest := filepath.Join(b.Dir, "overrides", filepath.FromSlash(rel))
		if toDir != "" {
			dest = filepath.Join(toDir, filepath.FromSlash(rel))
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := fsutil.Write(dest, data); err != nil {
			return nil, err
		}
		report.Pulled = append(report.Pulled, rel+" -> "+b.relPath(dest))
		pulled = append(pulled, rel)
	}
	for _, f := range plans {
		if !drifted(f) || (len(named) > 0 && !named[f.rel]) {
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
			dest = filepath.Join(b.Dir, "overrides", filepath.FromSlash(f.rel))
		case src.owned != nil:
			if _, ok := src.owned.(propsFile); !ok {
				report.Skipped = append(report.Skipped, f.rel+" (player files are managed by `shulker player`)")
				continue
			}
			if pf := src.owned.(propsFile); pf.origins != nil {
				wrote, err := b.pullOverrideKeys(f.rel, pf, f.merge.kept, existing, report)
				if err != nil {
					return nil, err
				}
				if wrote {
					pulled = append(pulled, f.rel)
				}
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
		if toDir != "" {
			dest = filepath.Join(toDir, filepath.FromSlash(f.rel))
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
		if err := fsutil.Write(dest, existing); err != nil {
			return nil, err
		}
		report.Pulled = append(report.Pulled, f.rel+" -> "+b.relPath(dest))
		pulled = append(pulled, f.rel)
	}
	if len(pulled) == 0 {
		return report, nil
	}
	desired, _, err = b.collect(side, opts, &Report{})
	if err != nil {
		return nil, err
	}
	for _, rel := range pulled {
		src := desired[rel]
		hash, err := b.hashSource(src)
		if err != nil {
			return nil, err
		}
		prev.Files[rel] = hash
		if src.owned != nil {
			prev.record(rel, src.owned)
		}
	}
	if err := b.saveState(dir, prev); err != nil {
		return nil, err
	}
	return report, nil
}

func (b *Builder) pullDest(side, to string) (string, error) {
	if to == "" {
		return "", nil
	}
	if manifest.IsSide(to) {
		return filepath.Join(b.Dir, to+"-overrides"), nil
	}
	features := slices.Sorted(maps.Keys(b.Manifest.Features))
	f, ok := b.Manifest.Features[to]
	if !ok {
		e := out.Errorf("usage", "%q is neither a side nor a feature", to)
		e.Candidates, e.Given, e.Flag = slices.Concat(manifest.SideNames, features), to, "--to"
		return "", e
	}
	folders := featureFolders(to, f, side)
	if len(folders) == 0 {
		return "", out.Errorf("usage", "feature %s has no override folder for the %s side", to, side)
	}
	return filepath.Join(b.Dir, folders[len(folders)-1]), nil
}

type drift struct {
	dir      string
	desired  map[string]source
	prev     State
	plans    []planned
	warnings []string
}

func (b *Builder) drift(side string, opts Options) (*drift, error) {
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Join(b.Dir, b.Manifest.BuildDir(side))
	}
	report := &Report{Warnings: []string{}}
	desired, _, err := b.collect(side, opts, report)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if opts.Dir != "" {
			e := out.Errorf("into-missing", "%s does not exist", dir)
			e.Flag = "--into"
			return nil, e
		}
		return nil, out.Errorf("not-built", "%s has no build directory; run `shulker build`", side)
	}
	prev, stateErr := ReadState(dir)
	if stateErr != nil {
		report.Warnings = append(report.Warnings, stateErr.Error())
	}
	plans, err := b.plan(dir, desired, prev, false)
	if err != nil {
		return nil, err
	}
	return &drift{dir: dir, desired: desired, prev: prev, plans: plans, warnings: report.Warnings}, nil
}

func drifted(f planned) bool {
	if f.src.owned != nil {
		return len(f.merge.kept) > 0
	}
	return f.state == stateKept || f.state == stateConflict || f.state == stateUntracked || f.state == stateOrphan
}

func driftedPaths(plans []planned) []string {
	var paths []string
	for _, f := range plans {
		if drifted(f) {
			paths = append(paths, f.rel)
		}
	}
	return paths
}

func (b *Builder) pullOverrideKeys(rel string, pf propsFile, kept map[string]bool, existing []byte, report *PullReport) (bool, error) {
	current := parseProperties(existing)
	byFile := map[string]properties{}
	wrote := false
	for _, k := range sortedKeys(kept) {
		o := pf.origins[k]
		switch {
		case o.path == "":
			if raw := b.manifestBlock(rel); raw != nil {
				raw[k] = typedProperty(raw[k], current[k])
				report.Keys = append(report.Keys, rel+" "+k+"="+current[k])
				report.ManifestChanged = true
				wrote = true
			}
		case o.pack != "":
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s %s (comes from pack %s)", rel, k, o.pack))
		case o.template:
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s %s (rendered from template %s)", rel, k, b.relPath(o.path)))
		default:
			if byFile[o.path] == nil {
				byFile[o.path] = properties{}
			}
			byFile[o.path][k] = current[k]
		}
	}
	for _, dest := range slices.Sorted(maps.Keys(byFile)) {
		if err := writeProperties(dest, byFile[dest]); err != nil {
			return false, err
		}
		for _, k := range byFile[dest].keys() {
			report.Pulled = append(report.Pulled, rel+" "+k+" -> "+b.relPath(dest))
		}
		wrote = true
	}
	return wrote, nil
}

func writeProperties(dest string, set properties) error {
	data, err := os.ReadFile(dest)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return fsutil.Write(dest, set.mergeInto(data, "=", nil))
}

func (b *Builder) adoptKeys(rel string, keys []string, d *drift, report *PullReport) error {
	if !strings.HasSuffix(rel, ".properties") {
		return out.Errorf("usage", "--key works on .properties files, not %s", rel)
	}
	src := d.desired[rel]
	if _, merged := src.owned.(propsFile); !merged && src.origin != "" {
		return out.Errorf("usage", "%s is copied whole (wholeFiles); edit the override instead", rel)
	}
	if src.template && src.pack == "" {
		return out.Errorf("usage", "%s is rendered from template %s; add the keys there by hand", rel, b.relPath(src.origin))
	}
	existing, err := os.ReadFile(filepath.Join(d.dir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return out.Errorf("file-not-found", "%s is not in %s", rel, d.dir)
	}
	if err != nil {
		return err
	}
	current := parseProperties(existing)
	set := properties{}
	for _, k := range keys {
		v, ok := current[k]
		if !ok {
			e := out.Errorf("key-not-found", "%s has no key %q", rel, k)
			e.Candidates, e.Given = current.keys(), k
			return e
		}
		set[k] = v
	}
	dest := src.origin
	if dest == "" || src.pack != "" {
		dest = filepath.Join(b.Dir, "overrides", filepath.FromSlash(rel))
	}
	if err := writeProperties(dest, set); err != nil {
		return err
	}
	for _, k := range set.keys() {
		report.Adopted = append(report.Adopted, rel+" "+k+" -> "+b.relPath(dest))
	}
	return nil
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
