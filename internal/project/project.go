// Package project opens a directory's shulker.json and shulker.lock together, and tells how far the
// lock has drifted from the manifest.
package project

import (
	"errors"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

var ErrNoManifest = &out.Error{Code: "manifest-not-found", Message: "no shulker.json here", Help: "run `shulker init`", Exit: out.ExitError}

type Project struct {
	Dir      string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
}

func Open(dir string) (*Project, error) {
	p := &Project{Dir: dir}
	m, err := manifest.Load(p.ManifestPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoManifest
	}
	if err != nil {
		return nil, err
	}
	p.Manifest = m
	l, err := lock.Load(p.LockPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	p.Lock = l
	return p, nil
}

func (p *Project) ManifestPath() string { return filepath.Join(p.Dir, manifest.FileName) }
func (p *Project) LockPath() string     { return filepath.Join(p.Dir, lock.FileName) }

func (p *Project) RequireLock() error {
	if p.Lock == nil {
		e := out.Errorf("lock-not-found", "no %s", lock.FileName)
		e.Help = "run `shulker lock`"
		return e
	}
	return nil
}

func (p *Project) SaveManifest() error { return p.Manifest.Save(p.ManifestPath()) }

func (p *Project) SaveLock() error { return p.Lock.Save(p.LockPath()) }
