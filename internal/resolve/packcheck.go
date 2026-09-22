package resolve

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"

	"shulker.sh/shulker/internal/manifest"
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
// loading: the game wants pack.mcmeta at a pack's root, with a description and a format, and Iris
// wants shaders/ at a shader's. The format is not held to the project's Minecraft version.
func packFolderProblems(dir, rel, kind string) []string {
	root := filepath.Join(dir, filepath.FromSlash(rel))
	if kind == manifest.TypeShader {
		if st, err := os.Stat(filepath.Join(root, "shaders")); err != nil || !st.IsDir() {
			return []string{rel + " has no shaders/ folder at its root, so Iris won't load it"}
		}
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root, "pack.mcmeta"))
	if errors.Is(err, os.ErrNotExist) {
		return []string{rel + " has no pack.mcmeta at its root, so the game won't load it"}
	}
	mcmeta := path.Join(rel, "pack.mcmeta")
	if err != nil || !json.Valid(data) {
		return []string{mcmeta + " is not valid JSON, so the game won't load it"}
	}
	var doc struct {
		Pack map[string]json.RawMessage `json:"pack"`
	}
	_ = json.Unmarshal(data, &doc)
	has := func(key string) bool {
		v, ok := doc.Pack[key]
		return ok && string(v) != "null"
	}
	var problems []string
	if !has("description") {
		problems = append(problems, mcmeta+" has no pack.description, so the game won't load it")
	}
	if !has("pack_format") && !(has("min_format") && has("max_format")) {
		problems = append(problems, mcmeta+" has no pack format (min_format and max_format, or pack_format), so the game won't load it")
	}
	return problems
}
