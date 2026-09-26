package project

import (
	"errors"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/out"
)

// Staged is a folder a new project is laid out in beside the one it is for, and moved into it
// only once it is whole, so a run that fails part way leaves that folder as it was.
type Staged struct {
	Dir  string
	into string
}

// Stage makes the staging folder for dir in dir's parent, which keeps the move a rename.
func Stage(dir string) (*Staged, error) {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+".import-")
	if err != nil {
		return nil, err
	}
	return &Staged{Dir: tmp, into: dir}, nil
}

// Commit moves what was staged into its folder: the whole staging folder when the folder doesn't
// exist yet, otherwise entry by entry, a staged file replacing one of the same name. Every entry
// is checked first, so a file where a folder is staged, or the other way round, moves nothing.
func (s *Staged) Commit() error {
	if _, err := os.Lstat(s.into); errors.Is(err, os.ErrNotExist) {
		return os.Rename(s.Dir, s.into)
	}
	if err := clashes(s.Dir, s.into); err != nil {
		return err
	}
	if err := moveInto(s.Dir, s.into); err != nil {
		return err
	}
	return os.RemoveAll(s.Dir)
}

// Discard removes the staging folder and whatever is left in it.
func (s *Staged) Discard() { os.RemoveAll(s.Dir) }

func clashes(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range entries {
		there, err := os.Stat(filepath.Join(to, e.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if there.IsDir() != e.IsDir() {
			e := out.Errorf("path-taken", "%s is already there as a %s", filepath.Join(to, e.Name()), kindOf(there))
			e.Help = "move it out of the way and run the command again"
			return e
		}
		if e.IsDir() {
			if err := clashes(filepath.Join(from, e.Name()), filepath.Join(to, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func kindOf(info os.FileInfo) string {
	if info.IsDir() {
		return "folder"
	}
	return "file"
}

func moveInto(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range entries {
		src, dst := filepath.Join(from, e.Name()), filepath.Join(to, e.Name())
		if e.IsDir() {
			if info, err := os.Stat(dst); err == nil && info.IsDir() {
				if err := moveInto(src, dst); err != nil {
					return err
				}
				continue
			}
		} else if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	return nil
}
