package local

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/out"
)

const FileName = "shulker.local.json"

type File struct {
	Features   map[string]bool   `json:"features,omitempty"`
	Targets    map[string]Target `json:"targets,omitempty"`
	DetectedOS string            `json:"detectedOs,omitempty"`

	dir    string
	exists bool
}

type Target struct {
	SyncDirs []string `json:"syncDirs,omitempty"`
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
		return nil, out.Errorf("local-invalid", "%s: %v", FileName, err)
	}
	f.exists = true
	return f, nil
}

func (f *File) Exists() bool { return f.exists }

func (f *File) Save() error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(f); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(f.dir, FileName), buf.Bytes(), 0o644); err != nil {
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

func (f *File) RecordSyncDir(target, dir string) bool {
	t := f.Targets[target]
	for _, d := range t.SyncDirs {
		if d == dir {
			return false
		}
	}
	t.SyncDirs = append(t.SyncDirs, dir)
	if f.Targets == nil {
		f.Targets = map[string]Target{}
	}
	f.Targets[target] = t
	return true
}

func (f *File) ExistingSyncDirs(target string) []string {
	var dirs []string
	for _, d := range f.Targets[target].SyncDirs {
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
	return true, os.WriteFile(path, data, 0o644)
}

func (f *File) Dir() string { return f.dir }
