// Package saves shares worlds between shulker's own instances. A save group is a folder under the
// saves root; an instance joins one by its saves/ being a link to that folder, so nothing is ever
// copied or merged.
package saves

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

const (
	// Default is the group every shulker instance joins until its savesGroup says otherwise.
	Default = "default"
	// None keeps an instance's worlds in its own saves/.
	None = "none"
	// DirName is the folder the game reads worlds from, inside an instance.
	DirName = "saves"

	timeLayout = "20060102-150405"
)

// IsValidGroup reports whether name can be a group: a registry-style key, so it is always a single
// safe folder name, and never None.
func IsValidGroup(name string) bool { return name != None && manifest.IsValidKey(name) }

// Result is what Link found and did. Worlds are the ones the instance's saves/ shows afterwards.
type Result struct {
	Group   string   `json:"group"`
	Changed bool     `json:"changed"`
	Moved   bool     `json:"moved,omitempty"`
	Worlds  []string `json:"worlds"`
	// Conflict says why saves/ was left as it was; the instance keeps its own worlds meanwhile.
	Conflict string `json:"conflict,omitempty"`
}

// Link makes dir's saves/ follow group under root, or hold its own worlds for None. Leaving a group
// leaves its worlds in the group. Joining one replaces an empty saves/, moves a saves/ holding worlds
// in as the group when the group has none, and otherwise leaves saves/ alone with a Conflict.
func Link(dir, root, group string) (Result, error) {
	path := filepath.Join(dir, DirName)
	res := Result{Group: group}
	current, linked := fsutil.ReadLink(path)
	if group == None {
		if linked {
			if err := fsutil.UnlinkDir(path); err != nil {
				return res, err
			}
			if err := os.MkdirAll(path, 0o755); err != nil {
				return res, err
			}
			res.Changed = true
		}
		return withWorlds(res, path)
	}

	want := filepath.Join(root, group)
	if linked && fsutil.SameTarget(path, current, want) {
		return withWorlds(res, path)
	}
	info, err := os.Lstat(path)
	switch {
	case linked:
		if err := fsutil.UnlinkDir(path); err != nil {
			return res, err
		}
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return res, err
	case !info.IsDir():
		res.Conflict = fmt.Sprintf("a file is in the way of %s; move it and sync again", path)
		return res, nil
	default:
		own, err := Worlds(path)
		if err != nil {
			return res, err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return res, err
		}
		if len(entries) > 0 {
			theirs, err := Worlds(want)
			if err != nil {
				return res, err
			}
			if len(theirs) > 0 {
				res.Conflict = fmt.Sprintf("%s and group %s both hold worlds; merge them by hand, then sync again", path, group)
				res.Worlds = own
				return res, nil
			}
			if err := moveInto(path, want); err != nil {
				res.Conflict = fmt.Sprintf("couldn't move %s into group %s (%v); move it by hand, then sync again", path, group, err)
				res.Worlds = own
				return res, nil
			}
			res.Moved = true
		} else if err := os.Remove(path); err != nil {
			return res, err
		}
	}
	if err := os.MkdirAll(want, 0o755); err != nil {
		return res, err
	}
	if err := fsutil.LinkDir(want, path); err != nil {
		return res, err
	}
	res.Changed = true
	return withWorlds(res, path)
}

// moveInto renames from to to, taking the place of an empty folder already there.
func moveInto(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Remove(to); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(from, to)
}

func withWorlds(res Result, path string) (Result, error) {
	worlds, err := Worlds(path)
	res.Worlds = worlds
	return res, err
}

// Worlds are the folders in dir holding a level.dat, which is what the game lists. A missing dir
// holds none.
func Worlds(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	worlds := []string{}
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "level.dat")); err == nil {
			worlds = append(worlds, e.Name())
		}
	}
	return worlds, nil
}

// Group is one save group's folder and what it holds.
type Group struct {
	Name   string `json:"name"`
	Dir    string `json:"dir"`
	Worlds int    `json:"worlds"`
	Size   int64  `json:"size"`
}

// Groups are the folders under root, by name. A missing root holds none.
func Groups(root string) ([]Group, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return []Group{}, nil
	}
	if err != nil {
		return nil, err
	}
	groups := []Group{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		worlds, err := Worlds(dir)
		if err != nil {
			return nil, err
		}
		size, err := Size(dir)
		if err != nil {
			return nil, err
		}
		groups = append(groups, Group{Name: e.Name(), Dir: dir, Worlds: len(worlds), Size: size})
	}
	return groups, nil
}

// Size is the total size of the regular files under dir.
func Size(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// Backup is one zip in a backups folder, named <time>[-<n>][-<instance>]-<reason>.zip: the time it
// was taken, a counter for a second backup in the same second, the instance that took it into a
// shared folder, and the command that took it. Its comment records the same and more, and wins.
type Backup struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	Taken    time.Time `json:"taken"`
	Reason   string    `json:"reason"`
	Instance string    `json:"instance,omitempty"`
	Size     int64     `json:"size"`
	Worlds   int       `json:"worlds"`
	// Names are the worlds the comment lists, when it had room for them.
	Names         []string `json:"names,omitempty"`
	Minecraft     string   `json:"minecraft,omitempty"`
	Loader        string   `json:"loader,omitempty"`
	LoaderVersion string   `json:"loaderVersion,omitempty"`
	seq           int
}

// Backups are the zips in dir, newest first. A missing dir holds none, and a zip with neither
// shulker's comment nor a name starting with a timestamp is not one of shulker's.
func Backups(dir string) ([]Backup, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, err
	}
	backups := []Backup{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".zip")
		if !ok || e.IsDir() {
			continue
		}
		b, ok, err := openBackup(filepath.Join(dir, e.Name()), id)
		if err != nil {
			return nil, err
		}
		if ok {
			backups = append(backups, b)
		}
	}
	slices.SortFunc(backups, func(x, y Backup) int {
		return cmp.Or(y.Taken.Compare(x.Taken), cmp.Compare(y.seq, x.seq), strings.Compare(y.ID, x.ID))
	})
	return backups, nil
}

// ReadBackup is the zip at path as a backup: its record when it is one of shulker's, and at least
// its name, path and size when it is any other zip.
func ReadBackup(path string) (Backup, error) {
	id := strings.TrimSuffix(filepath.Base(path), ".zip")
	b, ok, err := openBackup(path, id)
	if err != nil || ok {
		return b, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}
	return Backup{ID: id, Path: path, Size: info.Size()}, nil
}

func openBackup(path, id string) (Backup, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Backup{}, false, nil
	}
	if err != nil {
		return Backup{}, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Backup{}, false, err
	}
	b, ok := readBackup(f, info.Size(), id)
	b.Path = path
	return b, ok, nil
}

// Prune deletes all but the keep newest backups in dir and returns the ones it deleted, newest
// first.
func Prune(dir string, keep int) ([]Backup, error) {
	backups, err := Backups(dir)
	if err != nil {
		return nil, err
	}
	if len(backups) <= keep {
		return []Backup{}, nil
	}
	pruned := backups[keep:]
	for _, b := range pruned {
		if err := os.Remove(b.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return pruned, nil
}

// IsAutomatic says whether a backup of this reason was taken on the way past a change rather than
// asked for, which is what makes it one TrimAutomatic may delete.
func IsAutomatic(reason string) bool { return reason == "update" || reason == "sync" }

// TrimAutomatic deletes all but the keep newest automatic backups in dir. A backup someone asked
// for, and a zip shulker can't tell the reason of, never counts and is never deleted.
func TrimAutomatic(dir string, keep int) error {
	backups, err := Backups(dir)
	if err != nil {
		return err
	}
	kept := 0
	for _, b := range backups {
		if !IsAutomatic(b.Reason) {
			continue
		}
		if kept++; kept <= keep {
			continue
		}
		if err := os.Remove(b.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// PickBackup is the backup a restore puts back into t: the one named, else the nth of t's, newest
// first. A name with a path separator, or naming something on disk, is a path; any other is a
// name in t's backups.
func PickBackup(t Target, n int, named string) (Backup, error) {
	if named != "" {
		path := filepath.Join(t.Backups, strings.TrimSuffix(named, ".zip")+".zip")
		if _, err := os.Stat(named); err == nil || strings.ContainsRune(named, '/') || strings.ContainsRune(named, filepath.Separator) {
			if path, err = filepath.Abs(named); err != nil {
				return Backup{}, err
			}
		}
		b, err := ReadBackup(path)
		if errors.Is(err, fs.ErrNotExist) {
			return Backup{}, out.Errorf("backup-missing", "there is no backup %s", path)
		}
		return b, err
	}
	backups, err := Backups(t.Backups)
	if err != nil {
		return Backup{}, err
	}
	if len(backups) == 0 {
		return Backup{}, out.Errorf("backups-empty", "%s has no backups yet", t.WorldsDir)
	}
	if n > len(backups) {
		return Backup{}, out.Errorf("backup-missing", "there is no backup %d; %s has %s", n, t.WorldsDir, plural(len(backups), "backup", "backups"))
	}
	return backups[n-1], nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
