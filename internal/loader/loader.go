// Package loader is the one table of what shulker knows about each mod loader.
package loader

import "shulker.sh/shulker/internal/out"

type Loader struct {
	Name string
	// DependencyID is how a mod's metadata names the loader in its dependencies.
	DependencyID    string
	PrismUID        string
	MrpackKey       string
	CurseForgeType  string
	AlsoRuns        []string
	ServerLaunchJar string
	// InstallServerFlag, when set, means the server is set up by running the loader's own installer
	// jar with this flag and the server dir.
	InstallServerFlag string
	// InstallClientFlag, when set, means the launcher is set up by running the loader's own
	// installer jar with this flag and the launcher dir, instead of writing a meta profile json.
	InstallClientFlag string
	// MetadataFiles are the mod metadata files the loader reads from a jar, in the order it prefers them.
	MetadataFiles []string
	// MinecraftJarClassifier is the classifier the loader's installer expects on the vanilla server
	// jar it finds under libraries/: Forge looks for the bundled jar it would have downloaded,
	// NeoForge for the plain name.
	MinecraftJarClassifier string
	// MarkerFile is the file the marker jar declares itself in. Quilt reads Fabric's, so both use
	// it; the suffix picks the format, JSON for Fabric's and TOML for FML's.
	MarkerFile string
	// MavenPath is where the loader publishes its own jars, under both a Maven repository root and a
	// server dir's libraries/.
	MavenPath string
	// MavenVersionPrefixesGame means those jars are published under <game>-<version>, the way Forge
	// numbers its builds; NeoForge's build number already encodes the game version.
	MavenVersionPrefixesGame bool
}

// ArtifactVersion is the version the loader publishes its own jars under.
func (l Loader) ArtifactVersion(minecraft, version string) string {
	if l.MavenVersionPrefixesGame {
		return minecraft + "-" + version
	}
	return version
}

// CurseForgeModLoader is the loader id a CurseForge modpack manifest names. NeoForge's 1.20.1 builds
// kept Forge's numbering, and CurseForge tells them apart by the game version in the id.
func (l Loader) CurseForgeModLoader(minecraft, version string) string {
	if l.Name == "neoforge" && minecraft == "1.20.1" {
		return l.Name + "-1.20.1-" + version
	}
	return l.Name + "-" + version
}

var All = []Loader{
	{Name: "fabric", DependencyID: "fabricloader", PrismUID: "net.fabricmc.fabric-loader", MrpackKey: "fabric-loader", CurseForgeType: "4", ServerLaunchJar: "fabric-server-launch.jar", MetadataFiles: []string{"fabric.mod.json"}, MarkerFile: "fabric.mod.json"},
	{Name: "quilt", DependencyID: "quilt_loader", PrismUID: "org.quiltmc.quilt-loader", MrpackKey: "quilt-loader", CurseForgeType: "5", AlsoRuns: []string{"fabric"}, ServerLaunchJar: "quilt-server-launch.jar", MetadataFiles: []string{"quilt.mod.json", "fabric.mod.json"}, MarkerFile: "fabric.mod.json"},
	{Name: "neoforge", DependencyID: "neoforge", PrismUID: "net.neoforged", MrpackKey: "neoforge", CurseForgeType: "6", InstallServerFlag: "--install-server", InstallClientFlag: "--install-client", MetadataFiles: []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"}, MarkerFile: "META-INF/neoforge.mods.toml", MavenPath: "net/neoforged/neoforge"},
	{Name: "forge", DependencyID: "forge", PrismUID: "net.minecraftforge", MrpackKey: "forge", CurseForgeType: "1", InstallServerFlag: "--installServer", InstallClientFlag: "--installClient", MetadataFiles: []string{"META-INF/mods.toml"}, MarkerFile: "META-INF/mods.toml", MinecraftJarClassifier: "bundled", MavenPath: "net/minecraftforge/forge", MavenVersionPrefixesGame: true},
}

func Lookup(name string) (Loader, bool) {
	for _, l := range All {
		if l.Name == name {
			return l, true
		}
	}
	return Loader{}, false
}

func ByPrismUID(uid string) (Loader, bool) {
	for _, l := range All {
		if l.PrismUID == uid {
			return l, true
		}
	}
	return Loader{}, false
}

func ProviderLoaders(name string) []string {
	if l, ok := Lookup(name); ok {
		return append([]string{l.Name}, l.AlsoRuns...)
	}
	return []string{name}
}

func Names() []string {
	names := make([]string, len(All))
	for i, l := range All {
		names[i] = l.Name
	}
	return names
}

// Require rejects a loader shulker has no table entry for, which the schema enum already blocks in
// a manifest but a hand-edited lock can still name.
func Require(name string) (Loader, error) {
	l, ok := Lookup(name)
	if !ok {
		return l, out.Errorf("unsupported-loader", "shulker doesn't support the %s loader", name)
	}
	return l, nil
}
