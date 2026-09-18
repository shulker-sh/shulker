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
	Features   map[string]bool     `json:"features,omitempty"`
	SyncDirs   map[string][]string `json:"syncDirs,omitempty"`
	DetectedOS string              `json:"detectedOs,omitempty"`

	dir    string
	exists bool
}

func Load(dir string) (*File, error) {
	f := &File{dir: dir}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, f); err != nil {
		return nil, schema.Invalid("local-invalid", FileName, data, err)
	}
	f.exists = true
	return f, nil
}

func (f *File) Exists() bool { return f.exists }

func (f *File) Save() error {
	if err := fsutil.WriteJSON(filepath.Join(f.dir, FileName), f); err != nil {
		return err
	}
	f.exists = true
	return nil
}

func (f *File) SetFeature(name string, on bool) {
	if f.Features == nil {
		f.Features = map[string]bool{}
	}
	f.Features[name] = on
}

func (f *File) ResetFeature(name string) bool {
	_, ok := f.Features[name]
	delete(f.Features, name)
	return ok
}

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

func (f *File) ExistingSyncDirs(side string) []string {
	var dirs []string
	for _, d := range f.SyncDirs[side] {
		if _, err := os.Stat(d); !errors.Is(err, os.ErrNotExist) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

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
