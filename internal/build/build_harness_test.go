package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build/marker"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
)

// save writes the manifest and lock to the project dir, since a build records the lock file's hash.
func (p *project) save() {
	p.t.Helper()
	if err := p.b.Manifest.Save(filepath.Join(p.b.Dir, manifest.FileName)); err != nil {
		p.t.Fatal(err)
	}
	if err := p.b.Lock.Save(p.b.LockPath); err != nil {
		p.t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// file writes content at rel under the project dir.
func (p *project) file(rel, content string) {
	p.t.Helper()
	writeFile(p.t, filepath.Join(p.b.Dir, filepath.FromSlash(rel)), content)
}

// project reads the file at rel under the project dir, "" when there is none.
func (p *project) project(rel string) string {
	data, _ := os.ReadFile(filepath.Join(p.b.Dir, filepath.FromSlash(rel)))
	return string(data)
}

// lockDependency locks the version from h as key, required by the given mods rather than listed
// in the manifest.
func (p *project) lockDependency(key string, h *host, v provider.Version, requiredBy ...string) {
	p.t.Helper()
	p.lockMod(key, h, v)
	delete(p.b.Manifest.Requires, key)
	m := p.b.Lock.Mods[key]
	m.RequiredBy = requiredBy
	p.b.Lock.Mods[key] = m
}

func (p *project) build(side string, opts Options) (*Report, error) {
	p.t.Helper()
	p.save()
	return p.b.Build(side, opts)
}

func (p *project) mustBuild(side string, opts Options) *Report {
	p.t.Helper()
	report, err := p.build(side, opts)
	if err != nil {
		p.t.Fatalf("build %s: %v", side, err)
	}
	return report
}

func (p *project) diff(side string) *DiffReport {
	p.t.Helper()
	p.save()
	report, err := p.b.Diff(side, Options{})
	if err != nil {
		p.t.Fatalf("diff %s: %v", side, err)
	}
	return report
}

func (p *project) pull(side string, req PullRequest) (*PullReport, error) {
	p.t.Helper()
	p.save()
	return p.b.Pull(side, req, Options{})
}

func (p *project) mustPull(side string, req PullRequest) *PullReport {
	p.t.Helper()
	report, err := p.pull(side, req)
	if err != nil {
		p.t.Fatalf("pull %s %+v: %v", side, req, err)
	}
	return report
}

func (p *project) builtPath(side, rel string) string {
	return filepath.Join(p.b.Dir, "build", side, filepath.FromSlash(rel))
}

func (p *project) built(side, rel string) string {
	p.t.Helper()
	data, err := os.ReadFile(p.builtPath(side, rel))
	if err != nil {
		p.t.Fatalf("%s %s: %v", side, rel, err)
	}
	return string(data)
}

func (p *project) hasBuilt(side, rel string) bool {
	_, err := os.Stat(p.builtPath(side, rel))
	return err == nil
}

func (p *project) writeBuilt(side, rel, content string) {
	p.t.Helper()
	writeFile(p.t, p.builtPath(side, rel), content)
}

func (p *project) removeBuilt(side, rel string) {
	p.t.Helper()
	if err := os.Remove(p.builtPath(side, rel)); err != nil {
		p.t.Fatal(err)
	}
}

// mods lists the jars the client build placed, the marker aside.
func (p *project) mods() []string {
	p.t.Helper()
	entries, err := os.ReadDir(p.builtPath("client", "mods"))
	if err != nil && !os.IsNotExist(err) {
		p.t.Fatal(err)
	}
	marker := filepath.Base(marker.JarPath(p.b.Manifest.Name))
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jar") && e.Name() != marker {
			names = append(names, e.Name())
		}
	}
	return names
}

func contains(lines []string, text string) bool {
	for _, l := range lines {
		if strings.Contains(l, text) {
			return true
		}
	}
	return false
}
