package resolve

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/zipfile"
)

// checkPackFolder warns about each problem packFolderProblems finds when a local pack entry's file
// is a folder; the folder is locked all the same.
func (r *Resolver) checkPackFolder(key, kind string, entry manifest.Require) {
	if entry.File == "" {
		return
	}
	if st, err := os.Stat(filepath.Join(r.Dir, filepath.FromSlash(entry.File))); err != nil || !st.IsDir() {
		return
	}
	for _, problem := range packFolderProblems(r.Dir, entry.File, kind) {
		r.warnOnce(key + ": " + problem)
	}
}

// packFolderProblems says what keeps a resource pack or shader folder, rel under dir, from
// loading: the game wants pack.mcmeta at a pack's root, with a description and a format, and a shader mod
// wants shaders/ at a shader's. The format is not held to the project's Minecraft version.
func packFolderProblems(dir, rel, kind string) []string {
	root := filepath.Join(dir, filepath.FromSlash(rel))
	problems := symlinkProblems(root, rel)
	if kind == manifest.TypeShader {
		if !hasShaders(root) {
			problems = append(problems, rel+" has no shaders/ folder at its root, so no shader mod will load it")
		}
		return problems
	}
	if !hasPackMcmeta(root) {
		return append(problems, rel+" has no pack.mcmeta at its root, so the game won't load it")
	}
	data, err := os.ReadFile(filepath.Join(root, "pack.mcmeta"))
	mcmeta := path.Join(rel, "pack.mcmeta")
	if err != nil || !json.Valid(data) {
		return append(problems, mcmeta+" is not valid JSON, so the game won't load it")
	}
	var doc struct {
		Pack map[string]json.RawMessage `json:"pack"`
	}
	_ = json.Unmarshal(data, &doc)
	has := func(key string) bool {
		v, ok := doc.Pack[key]
		return ok && string(v) != "null"
	}
	if !has("description") {
		problems = append(problems, mcmeta+" has no pack.description, so the game won't load it")
	}
	if !has("pack_format") && !(has("min_format") && has("max_format")) {
		problems = append(problems, mcmeta+" has no pack format (min_format and max_format, or pack_format), so the game won't load it")
	}
	return problems
}

// symlinkProblems names each symlink the folder's zip follows, since what it points at lies outside
// the folder and can change without it.
func symlinkProblems(root, rel string) []string {
	files, err := zipfile.FolderFiles(root)
	if err != nil {
		return nil
	}
	var problems []string
	for _, f := range files {
		problem := path.Join(rel, f.Link) + " is a symlink, so the pack holds a copy of what it points at"
		if f.Link != "" && !slices.Contains(problems, problem) {
			problems = append(problems, problem)
		}
	}
	return problems
}

// hasPackMcmeta reports whether the folder root has the pack.mcmeta a resource pack keeps there.
func hasPackMcmeta(root string) bool {
	st, err := os.Stat(filepath.Join(root, "pack.mcmeta"))
	return err == nil && st.Mode().IsRegular()
}

// hasShaders reports whether the folder root has the shaders/ folder a shader keeps there.
func hasShaders(root string) bool {
	return IsLocalFolder(filepath.Join(root, "shaders"))
}
