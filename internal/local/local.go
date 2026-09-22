// Package local reads and writes shulker.local.json, a project's settings for this machine alone,
// which stay out of version control.
package local

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/schema"
)

const FileName = "shulker.local.json"

type File struct {
	Schema     string              `json:"$schema"`
	Features   map[string]bool     `json:"features,omitempty"`
	SyncDirs   map[string][]string `json:"syncDirs,omitempty"`
	DetectedOS string              `json:"detectedOs,omitempty"`

	dir      string
	isOnDisk bool
}

// ReplacedError says Load moved aside a file it couldn't read, or tried to. The file Load returns
// with it is empty and usable: the caller warns and carries on.
type ReplacedError struct {
	path, kept string
	reason     string
	newer      bool
	moveErr    error
}

// Newer reports whether a newer shulker wrote the file, whose fix is `shulker self update`.
func (e *ReplacedError) Newer() bool { return e.newer }

func (e *ReplacedError) Error() string {
	msg := e.path + " " + e.reason + "; moved it to " + e.kept + " and using the manifest's feature defaults"
	if e.moveErr != nil {
		msg = e.path + " " + e.reason + "; couldn't move it aside, so using the manifest's feature defaults (" + e.moveErr.Error() + ")"
	}
	if e.newer && e.moveErr == nil {
		msg += ". Move it back after updating shulker to keep those settings"
	}
	return msg
}

// Load reads dir's shulker.local.json. One that is corrupt, foreign, lacks its $schema or was written
// by a newer shulker is renamed to shulker.local.json.replaced, and Load returns an empty file with a
// *ReplacedError, since this per-machine file never stops a command.
func Load(dir string) (*File, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{dir: dir}, nil
	}
	if err != nil {
		return nil, err
	}
	got, want, err := schema.ReadMarker(schema.Local, data)
	var reason string
	switch {
	case err != nil:
		reason = "is unreadable (" + err.Error() + ")"
	case got > want:
		reason = schema.Newer(got, want)
	default:
		f := &File{dir: dir}
		if err := json.Unmarshal(data, f); err != nil {
			reason = "is unreadable (" + err.Error() + ")"
			break
		}
		f.isOnDisk = true
		return f, nil
	}
	e := &ReplacedError{path: path, kept: path + ".replaced", reason: reason, newer: got > want}
	e.moveErr = os.Rename(e.path, e.kept)
	return &File{dir: dir}, e
}

func (f *File) Exists() bool { return f.isOnDisk }

func (f *File) Save() error {
	f.Schema = schema.URL(schema.Local)
	if err := fsutil.WriteJSON(filepath.Join(f.dir, FileName), f); err != nil {
		return err
	}
	f.isOnDisk = true
	return nil
}

func (f *File) SetFeature(name string, on bool) {
	if f.Features == nil {
		f.Features = map[string]bool{}
	}
	f.Features[name] = on
}

// ResetFeature drops this machine's setting for a feature, and reports whether it had one.
func (f *File) ResetFeature(name string) bool {
	_, ok := f.Features[name]
	delete(f.Features, name)
	return ok
}

// RecordSyncDir remembers a directory a side was synced into, and reports whether it was new.
func (f *File) RecordSyncDir(side, dir string) bool {
	if slices.Contains(f.SyncDirs[side], dir) {
		return false
	}
	if f.SyncDirs == nil {
		f.SyncDirs = map[string][]string{}
	}
	f.SyncDirs[side] = append(f.SyncDirs[side], dir)
	return true
}

func (f *File) RemoveSyncDir(side, dir string) bool {
	dirs := f.SyncDirs[side]
	i := slices.Index(dirs, dir)
	if i < 0 {
		return false
	}
	dirs = append(dirs[:i:i], dirs[i+1:]...)
	if len(dirs) == 0 {
		delete(f.SyncDirs, side)
	} else {
		f.SyncDirs[side] = dirs
	}
	return true
}

// ExistingSyncDirs are a side's recorded sync directories that haven't since been deleted.
func (f *File) ExistingSyncDirs(side string) []string {
	var dirs []string
	for _, d := range f.SyncDirs[side] {
		if _, err := os.Stat(d); !errors.Is(err, os.ErrNotExist) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// AddToGitignore lists shulker.local.json in the project's .gitignore, when there is one and it
// doesn't already, and reports whether it changed the file.
func AddToGitignore(dir string) (bool, error) {
	path := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == FileName || line == "/"+FileName {
			return false, nil
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, "/"+FileName+"\n"...)
	return true, fsutil.Write(path, data)
}

func (f *File) Dir() string { return f.dir }
