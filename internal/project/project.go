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
	"shulker.sh/shulker/internal/pack"
)

var ErrNoManifest = &out.Error{Code: "manifest-not-found", Message: "no shulker.json here", Help: "run `shulker init`", Exit: out.ExitError}

type Project struct {
	Dir      string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
	// UnreadableLock is why the lock on disk couldn't be read, when OpenReplacingLock opened the
	// project without it.
	UnreadableLock error
	// ReplacedLock is where SaveLock kept the unreadable lock it wrote over.
	ReplacedLock string
	// Packs are the modpacks the command has read for the project, for its later steps to reuse.
	Packs *OpenedPacks
}

// OpenedPacks are the modpacks a command read for a project, and whether a relock read them.
type OpenedPacks struct {
	Loaded      []*pack.Loaded
	IsRelocking bool
}

// Open fails on a lock it can't read; OpenReplacingLock tolerates one.
func Open(dir string) (*Project, error) {
	p, err := open(dir)
	if err != nil {
		return nil, err
	}
	if p.UnreadableLock != nil {
		e := out.AsError(p.UnreadableLock)
		if e.Code == "lock-invalid" {
			e.Help = "run `shulker lock`"
		}
		return nil, e
	}
	return p, nil
}

// OpenReplacingLock opens dir with no lock when the one there can't be read, so the next SaveLock
// writes a fresh one over it.
func OpenReplacingLock(dir string) (*Project, error) {
	return open(dir)
}

func open(dir string) (*Project, error) {
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
	switch {
	case errors.Is(err, os.ErrNotExist):
	case out.CodeOf(err) == "lock-invalid", out.CodeOf(err) == "schema-newer":
		p.UnreadableLock = err
	case err != nil:
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

func (p *Project) SaveLock() error {
	if p.UnreadableLock == nil || p.ReplacedLock != "" {
		return p.Lock.Save(p.LockPath())
	}
	kept, err := p.Lock.Replace(p.LockPath())
	p.ReplacedLock = kept
	return err
}
