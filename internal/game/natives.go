package game

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
)

// ExtractNatives unpacks the assembly's native jars into dir, which is the instance's own natives
// directory: the jars are shared in the store, but what comes out of them is per-instance, because
// the game is handed one directory to load its binaries from.
func (a Assembly) ExtractNatives(s Store, dir string, p Platform) error {
	if len(a.Natives) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range a.Natives {
		if err := unpack(s.Local(f), dir, a.Excludes[f.Path], p); err != nil {
			return out.Errorf("store-incomplete", "shulker can't unpack %s", filepath.Base(f.Path)).WithCause("zip", err)
		}
	}
	return nil
}

func unpack(jar, dir string, exclude []string, p Platform) error {
	r, err := zip.OpenReader(jar)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, entry := range r.File {
		if entry.FileInfo().IsDir() || excluded(entry.Name, exclude) {
			continue
		}
		name := nativeName(entry.Name, p)
		path := filepath.Join(dir, filepath.FromSlash(name))
		if !strings.HasPrefix(path, dir+string(os.PathSeparator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := writeEntry(entry, path); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(entry *zip.File, path string) error {
	f, err := entry.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	if err := fsutil.WriteFrom(path, f); err != nil {
		return err
	}
	return os.Chmod(path, entry.Mode().Perm()|0o600)
}

func excluded(name string, exclude []string) bool {
	for _, prefix := range exclude {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// nativeName renames the .jnilib files LWJGL 2 shipped for macOS to .dylib, which is the only
// extension a Java 8 or newer runtime will load them under.
func nativeName(name string, p Platform) string {
	if p.OS == "osx" && strings.HasSuffix(name, ".jnilib") {
		return strings.TrimSuffix(name, ".jnilib") + ".dylib"
	}
	return name
}
