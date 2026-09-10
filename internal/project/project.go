package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shulker-sh/shulker/internal/lock"
	"github.com/shulker-sh/shulker/internal/manifest"
)

var ErrNoManifest = errors.New("no shulker.json here; run `shulker init`")

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
		return fmt.Errorf("no %s; run `shulker install` after `shulker init`", lock.FileName)
	}
	return nil
}

func (p *Project) LockStale() bool {
	if p.Lock == nil {
		return true
	}
	h, err := p.Manifest.ResolutionSha256()
	return err != nil || h != p.Lock.ManifestSha256
}

func (p *Project) SaveManifest() error { return p.Manifest.Save(p.ManifestPath()) }

func (p *Project) SaveLock() error {
	h, err := p.Manifest.ResolutionSha256()
	if err != nil {
		return err
	}
	p.Lock.ManifestSha256 = h
	return p.Lock.Save(p.LockPath())
}
