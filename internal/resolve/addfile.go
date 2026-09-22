package resolve

import (
	"archive/zip"
	"bytes"
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
	"shulker.sh/shulker/internal/zipfile"
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

// IsLocalFolder reports whether an add argument names a folder, which a resource pack or shader
// can be built from.
func IsLocalFolder(arg string) bool {
	st, err := os.Stat(arg)
	return err == nil && st.IsDir()
}

// addFile adds the file or pack folder at path as a local file entry and locks it. One outside the
// project, or in a folder whose files something else owns, is copied into manifest.FilesDir and the
// entry names the copy.
func (r *Resolver) addFile(ctx context.Context, path string, opts AddOptions) error {
	for _, flag := range []struct{ name, value string }{{"pin", opts.Pin}, {"channel", opts.Channel}, {"provider", opts.Provider}} {
		if flag.value != "" {
			return out.Errorf("usage", "--%s doesn't apply to a local file", flag.name)
		}
	}
	path = filepath.Clean(path)
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() && !st.IsDir() {
		return out.Errorf("file-not-found", "%s is not a file", path)
	}
	folder := st.IsDir()
	var kind string
	if folder {
		kind, err = folderKind(path, opts.Type)
	} else {
		kind, err = fileKind(path, opts.Type)
	}
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
		if info, err = jarmeta.Read(path, path, r.Lock.Loader.Type); err != nil {
			return prefixed("mod "+filepath.Base(path), err)
		}
		if key == "" {
			key = info.ID
		}
	} else if key == "" && folder {
		key = nameKey(filepath.Base(path))
	} else if key == "" {
		key = StemKey(path)
	}
	if !manifest.IsValidKey(key) {
		e := out.Errorf("usage", "%s can't be a requires key", key)
		e.Help = fmt.Sprintf("pass `--as <key>` to give this %s one", kind)
		return e
	}
	rel, copied, err := r.ProjectPath(path)
	if err != nil {
		return err
	}
	if folder && copied {
		if err := r.notHoldingProject(path); err != nil {
			return err
		}
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
		if name := filepath.Base(path); opts.KeepFilename && kind != manifest.TypeMod && !folder && strings.HasSuffix(name, ".zip") && name != key+".zip" {
			entry.Filename = name
		}
	}
	if opts.Side != "" {
		entry.Side = opts.Side
	}
	if copied {
		if err := r.CopyIn(path, rel, isReadded); err != nil {
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

// CopyIn copies the file or pack folder at path to rel in the project. A re-add refreshes the copy
// its own entry names, which is how a rebuilt jar gets back in; otherwise a different one already
// at rel is refused rather than replaced.
func (r *Resolver) CopyIn(path, rel string, isReadded bool) error {
	to := filepath.Join(r.Dir, filepath.FromSlash(rel))
	if IsLocalFolder(path) {
		r.log("copying %s/ into %s/", filepath.Base(path), manifest.FilesDir)
		if !isReadded {
			if err := sameFolderIfAny(path, to); err != nil {
				return err
			}
		}
		return copyFolder(path, to)
	}
	r.log("copying %s into %s/", filepath.Base(path), manifest.FilesDir)
	if !isReadded {
		if err := sameFileIfAny(path, to); err != nil {
			return err
		}
	}
	return copyFile(path, to)
}

// notHoldingProject refuses a folder the project lies in, whose copy into the project would copy
// itself without end.
func (r *Resolver) notHoldingProject(path string) error {
	root, err := filepath.EvalSymlinks(r.Dir)
	if err != nil {
		return err
	}
	folder, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(folder, root); err == nil && filepath.IsLocal(rel) {
		return out.Errorf("usage", "%s holds this project, so it can't be copied into it", filepath.Base(path))
	}
	return nil
}

// fileKind is what a local file is, refused unless its name ends the way the manifest holds that
// kind's file to.
func fileKind(path, asked string) (string, error) {
	kind, err := guessFileKind(path, asked)
	if err != nil {
		return "", err
	}
	if want := manifest.FileExtension(kind); !strings.HasSuffix(path, want) {
		e := out.Errorf("usage", "%s isn't a %s, so it can't be added as a %s", filepath.Base(path), want, kind)
		e.Help = "rename it to end in " + want
		return "", e
	}
	return kind, nil
}

// guessFileKind is the type asked for, a mod for a jar, and for a zip whatever it holds.
func guessFileKind(path, asked string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".mrpack" {
		e := out.Errorf("usage", "%s is a modpack archive", filepath.Base(path))
		e.Help = "add it with `shulker modpack add`"
		return "", e
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
	return "", typeAmbiguous(path, manifest.TypeMod, manifest.TypeResourcePack, manifest.TypeShader)
}

// folderKind is what a pack folder is: the type asked for, and otherwise whatever its root holds.
// Only a resource pack or a shader can be built from a folder.
func folderKind(path, asked string) (string, error) {
	switch asked {
	case manifest.TypeResourcePack, manifest.TypeShader:
		return asked, nil
	case "":
	default:
		e := out.Errorf("usage", "%s is a folder, and only a resource pack or shader can be built from one", filepath.Base(path))
		e.Help = "add a mod as its jar"
		return "", e
	}
	if hasPackMcmeta(path) {
		return manifest.TypeResourcePack, nil
	}
	if hasShaders(path) {
		return manifest.TypeShader, nil
	}
	return "", typeAmbiguous(path, manifest.TypeResourcePack, manifest.TypeShader)
}

func typeAmbiguous(path string, candidates ...string) error {
	e := out.Errorf("type-ambiguous", "%s holds no resource pack or shader shulker recognises", filepath.Base(path))
	e.Candidates = candidates
	e.Flag = "--type"
	return e
}

// StemKey is a requires key made from a file's name without its extension.
func StemKey(path string) string {
	return nameKey(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
}

// nameKey is a requires key made from a name: lowercased, with anything a key can't hold made a
// dash.
func nameKey(name string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(name) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' {
			b.WriteRune(c)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// ProjectPath is the relative path an entry names the file at path by, and whether the file has
// to be copied there first.
func (r *Resolver) ProjectPath(path string) (string, bool, error) {
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
	return r.modIDFree(info.ID, key)
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

// copyFolder replaces the folder at to with a copy of the one at from, leaving out what a pack
// folder's zip leaves out, so a file gone from from is gone from the copy too.
func copyFolder(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	// The copy is made beside to and swapped in, since from may be to itself, reached through a
	// symlink, and removing to first would remove it.
	staged, err := os.MkdirTemp(filepath.Dir(to), "."+filepath.Base(to)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	if err := os.Chmod(staged, 0o755); err != nil {
		return err
	}
	if err := copyTree(from, staged); err != nil {
		return err
	}
	if err := os.RemoveAll(to); err != nil {
		return err
	}
	return os.Rename(staged, to)
}

func copyTree(from, to string) error {
	files, err := zipfile.FolderFiles(from)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := copyFile(f.Path, filepath.Join(to, filepath.FromSlash(f.Name))); err != nil {
			return err
		}
	}
	return nil
}

// sameFolderIfAny refuses anything already at to but a folder that zips to the same bytes, since
// another entry may name it.
func sameFolderIfAny(from, to string) error {
	st, err := os.Stat(to)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.IsDir() {
		want, err := zipfile.Folder(from)
		if err != nil {
			return err
		}
		have, err := zipfile.Folder(to)
		if err != nil {
			return err
		}
		if bytes.Equal(want, have) {
			return nil
		}
	}
	return fileTaken(to)
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
	return fileTaken(to)
}

func fileTaken(to string) error {
	e := out.Errorf("file-taken", "%s already holds a different %s", manifest.FilesDir, filepath.Base(to))
	e.Help = "rename it, or remove the one in " + manifest.FilesDir + "/ first"
	return e
}
