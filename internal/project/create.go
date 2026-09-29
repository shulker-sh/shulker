package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/security"
)

// NewManifest is the manifest a new project starts from: name defaults to dir's own name,
// minecraft to the latest release, and side decides which side block it declares.
func NewManifest(name, dir, minecraft string, l manifest.Loader, side string) *manifest.Manifest {
	if name == "" {
		name = Slugify(filepath.Base(dir))
	}
	m := &manifest.Manifest{
		Schema:    manifest.SchemaURL,
		Name:      name,
		Authors:   DefaultAuthors(),
		Minecraft: OrLatest(minecraft),
		Loader:    l,
		Requires:  map[string]manifest.Require{},
	}
	switch side {
	case "server":
		m.Server = &manifest.Server{Memory: manifest.DefaultServerMemory, Properties: map[string]any{"difficulty": "easy"}}
	case "client":
		m.Client = NewClient()
	}
	return m
}

// Scaffold gives an empty project folder what every project has: an overrides folder and a
// .gitignore for what a build makes in place.
func Scaffold(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "overrides"), 0o755); err != nil {
		return err
	}
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, os.ErrNotExist) {
		return fsutil.Write(gi, []byte("/build/\n/data/\n/downloads/\n/shulker.local.json\n/.shulker/\n"))
	}
	return nil
}

// Create lays a new project out at dir: the scaffold, each override file under its layer, then
// the manifest and lock.
func Create(dir string, m *manifest.Manifest, l *lock.Lock, overrides []packarchive.Override) error {
	if err := Scaffold(dir); err != nil {
		return err
	}
	for _, o := range overrides {
		if !filepath.IsLocal(filepath.FromSlash(o.Path)) {
			return security.Refusal(security.Paths, out.Errorf("path-outside", "%s/%s is outside its folder", o.Layer, o.Path))
		}
		abs := filepath.Join(dir, o.Layer, filepath.FromSlash(o.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := fsutil.Write(abs, o.Data); err != nil {
			return err
		}
	}
	p := &Project{Dir: dir, Manifest: m, Lock: l}
	if err := p.SaveManifest(); err != nil {
		return err
	}
	return p.SaveLock()
}

// WriteIcon puts an archive's icon where m names it, and drops the key from a manifest whose
// archive carried none, so the project stays valid.
func WriteIcon(dir string, m *manifest.Manifest, icon []byte) error {
	if m.Icon == "" {
		return nil
	}
	if icon == nil {
		m.Icon = ""
		return nil
	}
	path := filepath.Join(dir, filepath.FromSlash(m.Icon))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.Write(path, icon)
}

// DefaultAuthors is who a new manifest credits: the git user, when git knows one, then shulker.sh.
func DefaultAuthors() []string {
	authors := []string{"shulker.sh"}
	name, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return authors
	}
	if user := strings.TrimSpace(string(name)); user != "" {
		authors = append([]string{user}, authors...)
	}
	return authors
}
