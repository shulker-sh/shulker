// Package integrations is shulker's knowledge of specific mods it acts on by role: the shader mods
// whose config a build writes, and the datapack loaders whose folder it fills. It is data, one list
// per role in order of preference, plus small methods on plain types; the behaviour stays with its
// callers, so this package imports nothing that knows about builds or locks.
package integrations

import (
	"slices"

	"shulker.sh/shulker/internal/version/minecraft"
)

// Shader is a mod that loads shader packs.
type Shader struct {
	// ID names the integration in the lock's shader loaders, the manifest's integrations and messages.
	ID string
	// JarIDs are the jar ids the mod is recognised by.
	JarIDs []string
	Name   string
	// Key is the argument to shulker add that adds the mod.
	Key string
	// Config is the properties file the mod reads its shaderPack and enableShaders from, "" when
	// it has none shulker can write.
	Config string
}

// Shaders are the shader mods, in the order enabling a pack tries them.
var Shaders = []Shader{
	{ID: "iris", JarIDs: []string{"iris"}, Name: "Iris", Key: "iris", Config: "config/iris.properties"},
	{ID: "oculus", JarIDs: []string{"oculus"}, Name: "Oculus", Key: "oculus", Config: "config/oculus.properties"},
	{ID: "canvas", JarIDs: []string{"canvas"}, Name: "Canvas", Key: "canvas"},
}

// Loads reports whether s is among present and can load a pack whose loaders are packLoaders:
// they name s, or name none.
func (s Shader) Loads(packLoaders []string, present map[string]bool) bool {
	return present[s.ID] && (len(packLoaders) == 0 || slices.Contains(packLoaders, s.ID))
}

// DatapackLoader is a mod that loads datapacks into every world from a folder outside them.
type DatapackLoader struct {
	ID     string
	JarIDs []string
	Name   string
	Key    string
	// Folders are where the mod reads datapacks from, oldest Minecraft version first.
	Folders []VersionedFolder
}

// VersionedFolder is a folder a datapack loader reads from Since a Minecraft version on, "" being
// every version before the next entry's.
type VersionedFolder struct {
	Since string
	Path  string
}

// DatapackLoaders are the global datapack mods, in the order a build prefers them.
var DatapackLoaders = []DatapackLoader{
	{ID: "paxi", JarIDs: []string{"paxi"}, Name: "Paxi", Key: "paxi", Folders: []VersionedFolder{{Path: "config/paxi/datapacks"}}},
	{ID: "openloader", JarIDs: []string{"openloader"}, Name: "Open Loader", Key: "open-loader", Folders: []VersionedFolder{{Path: "config/openloader/data"}, {Since: "1.21", Path: "config/openloader/packs"}}},
}

// Folder is where l reads datapacks from on Minecraft mc. A version that doesn't parse gets the
// newest folder.
func (l DatapackLoader) Folder(mc string) string {
	v, err := minecraft.Parse(mc)
	if err != nil {
		return l.Folders[len(l.Folders)-1].Path
	}
	folder := l.Folders[0].Path
	for _, f := range l.Folders[1:] {
		if v.Compare(minecraft.MustParse(f.Since)) >= 0 {
			folder = f.Path
		}
	}
	return folder
}

// IDs are every integration's id, shaders first.
func IDs() []string {
	var ids []string
	for _, s := range Shaders {
		ids = append(ids, s.ID)
	}
	for _, l := range DatapackLoaders {
		ids = append(ids, l.ID)
	}
	return ids
}

func jarIDs() map[string][]string {
	byID := map[string][]string{}
	for _, s := range Shaders {
		byID[s.ID] = s.JarIDs
	}
	for _, l := range DatapackLoaders {
		byID[l.ID] = l.JarIDs
	}
	return byID
}

// Match is the set of integration ids a build has, given the jar ids it places. overrides, the
// manifest's integrations, replaces the built-in jar ids of each integration it lists.
func Match(placed map[string]bool, overrides map[string][]string) map[string]bool {
	present := map[string]bool{}
	for id, jars := range jarIDs() {
		if o, listed := overrides[id]; listed {
			jars = o
		}
		if slices.ContainsFunc(jars, func(j string) bool { return placed[j] }) {
			present[id] = true
		}
	}
	return present
}
