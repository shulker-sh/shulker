package resolve

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

// IsLocalPath reports whether an add argument names a local file rather than a provider slug: a
// file that exists, or a name ending the way a jar or a pack archive does.
func IsLocalPath(arg string) bool {
	switch strings.ToLower(filepath.Ext(arg)) {
	case ".jar", ".zip", ".mrpack":
		return true
	}
	st, err := os.Stat(arg)
	return err == nil && st.Mode().IsRegular()
}

// addFile adds the file at path as a local file entry and locks it. A file outside the project, or
// in a folder whose files something else owns, is copied into manifest.FilesDir and the entry
// names the copy.
func (r *Resolver) addFile(ctx context.Context, path string, opts AddOptions) error {
	for _, flag := range []struct{ name, value string }{{"pin", opts.Pin}, {"channel", opts.Channel}, {"provider", opts.Provider}} {
		if flag.value != "" {
			return out.Errorf("usage", "--%s doesn't apply to a local file", flag.name)
		}
	}
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return out.Errorf("file-not-found", "%s is not a file", path)
	}
	kind, err := fileKind(path, opts.Type)
	if err != nil {
		return err
	}
	if kind != manifest.TypeMod && opts.Side != "" {
		return out.Errorf("usage", "--side applies to a mod, not a %s", kind)
	}
	key := opts.As
	var info *jarmeta.Info
	if kind == manifest.TypeMod {
		if r.Lock.Loader.Type == "" {
			return loaderRequired()
		}
		if info, err = jarmeta.Read(path, r.Lock.Loader.Type); err != nil {
			return prefixed("mod "+filepath.Base(path), err)
		}
		if key == "" {
			key = info.ID
		}
	} else if key == "" {
		key = stemKey(path)
	}
	if !manifest.IsValidKey(key) {
		e := out.Errorf("usage", "%s can't be a requires key", key)
		e.Help = fmt.Sprintf("pass `--as <key>` to give this %s one", kind)
		return e
	}
	rel, copied, err := r.projectPath(path)
	if err != nil {
		return err
	}
	entry, isReadded := r.Manifest.Requires[key]
	if isReadded && (entry.Kind() != kind || entry.File != rel) {
		return manifest.KeyTaken(key, entry.Kind(), kind)
	}
	if !isReadded {
		if err := r.fileKeyFree(key, kind, info); err != nil {
			return err
		}
		entry = manifest.Require{File: rel}
		if kind != manifest.TypeMod {
			entry.Type = kind
		}
	}
	if opts.Side != "" {
		entry.Side = opts.Side
	}
	if copied {
		r.log("copying %s into %s/", filepath.Base(path), manifest.FilesDir)
		to := filepath.Join(r.Dir, filepath.FromSlash(rel))
		// A re-add refreshes the copy its own entry names, which is how a rebuilt jar gets back in.
		if !isReadded {
			if err := sameFileIfAny(path, to); err != nil {
				return err
			}
		}
		if err := copyFile(path, to); err != nil {
			return err
		}
	}
	r.Manifest.Requires[key] = entry
	if kind != manifest.TypeMod {
		return r.lockFilePack(key, kind, entry)
	}
	dependents := r.Lock.Mods[key].RequiredBy
	if err := r.relockFile(ctx, r.Dir, key, entry, lock.Mod{}); err != nil {
		return err
	}
	for _, by := range dependents {
		r.Lock.AddRequiredBy(key, by)
	}
	return nil
}

// fileKind is what a local file is: the type asked for, a mod for a jar, and for a zip whatever it
// holds.
func fileKind(path, asked string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".mrpack" {
		return "", out.Errorf("requires-unsupported", "%s is a modpack archive, which shulker can't add yet", filepath.Base(path))
	}
	switch {
	case asked != "":
		return asked, nil
	case ext == ".jar":
		return manifest.TypeMod, nil
	}
	if zr, err := zip.OpenReader(path); err == nil {
		defer zr.Close()
		for _, f := range zr.File {
			switch {
			case f.Name == "pack.mcmeta":
				return manifest.TypeResourcePack, nil
			case strings.HasPrefix(f.Name, "shaders/"):
				return manifest.TypeShader, nil
			}
		}
	}
	e := out.Errorf("type-ambiguous", "%s holds no resource pack or shader shulker recognises", filepath.Base(path))
	e.Candidates = []string{manifest.TypeMod, manifest.TypeResourcePack, manifest.TypeShader}
	e.Flag = "--type"
	return "", e
}

// stemKey is a requires key made from a file's name without its extension.
func stemKey(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var b strings.Builder
	for _, c := range strings.ToLower(stem) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' {
			b.WriteRune(c)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// projectPath is the relative path an entry names the file at path by, and whether the file has
// to be copied there first.
func (r *Resolver) projectPath(path string) (string, bool, error) {
	root, err := filepath.EvalSymlinks(r.Dir)
	if err != nil {
		return "", false, err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", false, err
	}
	abs := filepath.Join(dir, filepath.Base(path))
	if rel, err := filepath.Rel(root, abs); err == nil && filepath.IsLocal(rel) && !r.ownedElsewhere(root, abs) {
		return filepath.ToSlash(rel), false, nil
	}
	return manifest.FilesDir + "/" + filepath.Base(path), true, nil
}

// ownedElsewhere reports whether abs lies where the files aren't the author's to keep: downloads/,
// which install sweeps; an override layer, which the build lays; and a side's build output.
func (r *Resolver) ownedElsewhere(root, abs string) bool {
	rel, _ := filepath.Rel(root, abs)
	top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	if top == DownloadsDir || top == "overrides" || strings.HasSuffix(top, "-overrides") {
		return true
	}
	for _, side := range r.Manifest.Sides() {
		if r.Manifest.BuildsInPlace(side) {
			if top == "mods" || top == "resourcepacks" || top == "shaderpacks" {
				return true
			}
			continue
		}
		build := r.Manifest.BuildDir(side)
		if !filepath.IsAbs(build) {
			build = filepath.Join(root, build)
		}
		if within, err := filepath.Rel(build, abs); err == nil && filepath.IsLocal(within) {
			return true
		}
	}
	return false
}

// fileKeyFree refuses a key the lock already holds for something else, and a jar whose id is
// locked under another key.
func (r *Resolver) fileKeyFree(key, kind string, info *jarmeta.Info) error {
	if kind != manifest.TypeMod {
		return r.packKeyFree(key, kind)
	}
	for _, other := range []string{manifest.TypeResourcePack, manifest.TypeShader} {
		if _, ok := r.packSection(other)[key]; ok {
			return manifest.KeyTaken(key, other, kind)
		}
	}
	if _, ok := r.Lock.Mods[key]; ok && r.Lock.JarID(key) != info.ID {
		e := out.Errorf("requires-taken", "requires already has %s as %s", key, r.Lock.JarID(key))
		e.Help = fmt.Sprintf("pass `--as <key>` to give %s another key", info.ID)
		return e
	}
	if held := r.lockedAs(info.ID); held != "" && held != key {
		return out.Errorf("requires-taken", "mod id %s is already locked as %s; a mod id can only be locked once", info.ID, held)
	}
	return nil
}

// copyFile writes the bytes of the file at from to to, replacing what is there.
func copyFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	f, err := os.Open(from)
	if err != nil {
		return err
	}
	defer f.Close()
	return fsutil.WriteFrom(to, f)
}

// sameFileIfAny refuses a different file already at to rather than replacing it, since another
// entry may name it.
func sameFileIfAny(from, to string) error {
	_, err := os.Stat(to)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return sameFile(from, to)
}

func sameFile(from, to string) error {
	want, err := fsutil.SHA512(from)
	if err != nil {
		return err
	}
	have, err := fsutil.SHA512(to)
	if err != nil {
		return err
	}
	if have == want {
		return nil
	}
	e := out.Errorf("file-taken", "%s already holds a different %s", manifest.FilesDir, filepath.Base(to))
	e.Help = "rename the file, or remove the one in " + manifest.FilesDir + "/ first"
	return e
}
