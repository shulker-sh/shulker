package saves

import (
	"archive/zip"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
)

// Archive is a zip of world folders opened to restore from. Worlds come from its entries, never
// from its comment, since they are what a restore unzips.
type Archive struct {
	Path   string
	Worlds []string
	zr     *zip.ReadCloser
}

// Restored is one world a restore put back, and whether it replaced one already there.
type Restored struct {
	Name     string `json:"name"`
	Replaced bool   `json:"replaced"`
}

// OpenArchive opens the zip at path, which must hold world folders at its root and nothing else.
func OpenArchive(path string) (*Archive, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, out.Errorf("backup-invalid", "%s won't open as a zip: %v", path, err)
	}
	a := &Archive{Path: path, zr: zr}
	if err := a.check(); err != nil {
		zr.Close()
		return nil, err
	}
	return a, nil
}

func (a *Archive) check() error {
	roots, levels := map[string]bool{}, map[string]bool{}
	for _, f := range a.zr.File {
		name := strings.TrimSuffix(f.Name, "/")
		if strings.Contains(name, `\`) || !filepath.IsLocal(name) {
			return out.Errorf("backup-invalid", "%s holds %s, which is outside its own folders", a.Path, f.Name)
		}
		root, rest, ok := strings.Cut(name, "/")
		if !ok && !f.FileInfo().IsDir() {
			return out.Errorf("backup-invalid", "%s holds the file %s at its root; a backup holds only world folders", a.Path, f.Name)
		}
		roots[root] = true
		if rest == "level.dat" {
			levels[root] = true
		}
	}
	for root := range roots {
		if !levels[root] {
			return out.Errorf("backup-invalid", "%s holds %s at its root, which is not a world: it has no level.dat", a.Path, root)
		}
	}
	if len(roots) == 0 {
		return out.Errorf("backup-invalid", "%s holds no worlds", a.Path)
	}
	for root := range roots {
		a.Worlds = append(a.Worlds, root)
	}
	slices.Sort(a.Worlds)
	return nil
}

// Close closes the zip.
func (a *Archive) Close() error { return a.zr.Close() }

// Restore puts worlds from the zip into dir, calling each before it unzips one. A world already in
// dir is removed and unzipped fresh, never merged, and worlds the list leaves out are not touched.
func (a *Archive) Restore(dir string, worlds []string, each func(world string)) ([]Restored, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	restored := make([]Restored, 0, len(worlds))
	for _, world := range worlds {
		if each != nil {
			each(world)
		}
		replaced, err := a.restoreWorld(dir, world)
		if err != nil {
			return restored, err
		}
		restored = append(restored, Restored{Name: world, Replaced: replaced})
	}
	return restored, nil
}

// restoreWorld unzips world beside its folder in dir and only then swaps it in, so a zip that fails
// part-way leaves the world as it was.
func (a *Archive) restoreWorld(dir, world string) (bool, error) {
	tmp, err := os.MkdirTemp(dir, ".restore-"+world+"-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o755); err != nil {
		return false, err
	}
	for _, f := range a.zr.File {
		rel, ok := strings.CutPrefix(f.Name, world+"/")
		if !ok {
			continue
		}
		path := filepath.Join(tmp, filepath.FromSlash(rel))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return false, err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			continue
		}
		if err := a.unzip(f, path); err != nil {
			return false, err
		}
	}
	dest := filepath.Join(dir, world)
	_, err = os.Lstat(dest)
	replaced := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.RemoveAll(dest); err != nil {
		return false, err
	}
	return replaced, os.Rename(tmp, dest)
}

func (a *Archive) unzip(f *zip.File, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	r, err := f.Open()
	if err != nil {
		return out.Errorf("backup-invalid", "%s: %s won't unzip: %v", a.Path, f.Name, err)
	}
	defer r.Close()
	return fsutil.WriteFrom(path, r)
}
