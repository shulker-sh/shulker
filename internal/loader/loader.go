// Package loader is the one table of what shulker knows about each mod loader.
package loader

import (
	"cmp"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/mcver"
	"shulker.sh/shulker/internal/out"
)

// ServerSetup is how a server gets its loader.
type ServerSetup int

const (
	// ServerVanilla is no loader: the server runs the vanilla jar.
	ServerVanilla ServerSetup = iota
	// ServerLauncher is Fabric's: a server launch jar that fetches the vanilla jar where it expects it.
	ServerLauncher
	// ServerProfile is Quilt's: a launch jar and the libraries its server profile lists.
	ServerProfile
	// ServerInstaller is NeoForge's and Forge's: the loader's own installer, run with InstallServerFlag.
	ServerInstaller
)

type Loader struct {
	Name string
	// Title is the loader's name as its own project writes it, for a name a player reads.
	Title string
	// DependencyID is how a mod's metadata names the loader in its dependencies.
	DependencyID string
	// ComponentUID is the loader's component uid in Prism's and MultiMC's mmc-pack.json.
	ComponentUID string
	// MrpackKey is the loader's key in an mrpack index's dependencies.
	MrpackKey string
	// CurseForgeType is CurseForge's numeric modLoaderType.
	CurseForgeType string
	// AlsoRuns are the loaders whose mods this one runs as well.
	AlsoRuns    []string
	ServerSetup ServerSetup
	// ServerLaunchJar is the jar a server built for this loader starts from.
	ServerLaunchJar string
	// InstallServerFlag is the flag the loader's own installer takes, with the server dir, to set up
	// a ServerInstaller server.
	InstallServerFlag string
	// InstallClientFlag, when set, means the launcher is set up by running the loader's own
	// installer jar with this flag and the launcher dir, instead of writing a meta profile json.
	InstallClientFlag string
	// MetadataFiles are the mod metadata files the loader reads from a jar, in the order it prefers them.
	MetadataFiles []string
	// ModAnnotations means the loader finds mods by the @Mod annotations in a jar's classes, with
	// MetadataFiles only filling in, as FML did before 1.13.
	ModAnnotations bool
	// MinecraftJarClassifier is the classifier the loader's installer expects on the vanilla server
	// jar it finds under libraries/: Forge looks for the bundled jar it would have downloaded,
	// NeoForge for the plain name.
	MinecraftJarClassifier string
	// MarkerFile is the file the marker jar declares itself in. Quilt reads Fabric's, so both use
	// it; the suffix picks the format, JSON for Fabric's and TOML for FML's. Empty means the loader
	// takes no marker.
	MarkerFile string
	// RootServerJars means the installer looks for the vanilla server jar in the server dir itself
	// and leaves the jar the server starts from there, rather than an args file under libraries/.
	RootServerJars bool
	// TopLevelMandatory means a jar in mods/ always loads as itself, so a nested copy of its id
	// never stands in for it.
	TopLevelMandatory bool
	// DependencyOverrides is the file in the game dir the loader reads dependency overrides from.
	DependencyOverrides string
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

var All = []Loader{
	{Name: "fabric", Title: "Fabric", DependencyID: "fabricloader", ComponentUID: "net.fabricmc.fabric-loader", MrpackKey: "fabric-loader", CurseForgeType: "4", ServerSetup: ServerLauncher, ServerLaunchJar: "fabric-server-launch.jar", MetadataFiles: []string{"fabric.mod.json"}, MarkerFile: "fabric.mod.json", DependencyOverrides: "config/fabric_loader_dependencies.json"},
	{Name: "quilt", Title: "Quilt", DependencyID: "quilt_loader", ComponentUID: "org.quiltmc.quilt-loader", MrpackKey: "quilt-loader", CurseForgeType: "5", AlsoRuns: []string{"fabric"}, ServerSetup: ServerProfile, ServerLaunchJar: "quilt-server-launch.jar", MetadataFiles: []string{"quilt.mod.json", "fabric.mod.json"}, MarkerFile: "fabric.mod.json", TopLevelMandatory: true},
	{Name: "neoforge", Title: "NeoForge", DependencyID: "neoforge", ComponentUID: "net.neoforged", MrpackKey: "neoforge", CurseForgeType: "6", ServerSetup: ServerInstaller, InstallServerFlag: "--install-server", InstallClientFlag: "--install-client", MetadataFiles: []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"}, MarkerFile: "META-INF/neoforge.mods.toml", MavenPath: "net/neoforged/neoforge"},
	{Name: "forge", Title: "Forge", DependencyID: "forge", ComponentUID: "net.minecraftforge", MrpackKey: "forge", CurseForgeType: "1", ServerSetup: ServerInstaller, InstallServerFlag: "--installServer", InstallClientFlag: "--installClient", MetadataFiles: []string{"META-INF/mods.toml"}, MarkerFile: "META-INF/mods.toml", MinecraftJarClassifier: "bundled", MavenPath: "net/minecraftforge/forge", MavenVersionPrefixesGame: true},
}

// For is the named loader as it runs on the given Minecraft version. Forge before 1.13 reads @Mod
// annotations and mcmod.info and has no marker jar, and its installers before 1.17 set no
// serverJarPath or args file.
func For(name, minecraft string) (Loader, bool) {
	l, ok := Lookup(name)
	if !ok || l.Name != "forge" {
		return l, ok
	}
	v, err := mcver.Parse(minecraft)
	if err != nil {
		return l, ok
	}
	if mcver.Compare(v, mcver.MustParse("1.13")) < 0 {
		l.MetadataFiles = []string{"mcmod.info"}
		l.ModAnnotations = true
		l.MarkerFile = ""
	}
	if mcver.Compare(v, mcver.MustParse("1.17")) < 0 {
		l.RootServerJars = true
		l.MinecraftJarClassifier = ""
	}
	return l, ok
}

// VanillaServerJar is the name a RootServerJars installer looks for the vanilla server jar under.
func VanillaServerJar(minecraft string) string {
	return "minecraft_server." + minecraft + ".jar"
}

// InstalledServerJar is the jar a RootServerJars installer leaves in the server dir to start from.
func (l Loader) InstalledServerJar(minecraft, version string) string {
	return l.Name + "-" + l.ArtifactVersion(minecraft, version) + ".jar"
}

// firstModernForge is the first Forge build whose installer shulker can run offline: every earlier
// one ships the legacy installer, which reads another install profile layout.
const firstModernForge = "14.23.5.2851"

// Supports reports whether shulker can set up the named loader's version on the given Minecraft
// version.
func Supports(name, minecraft, version string) error {
	if name != "forge" {
		return nil
	}
	v, err := mcver.Parse(minecraft)
	if err != nil {
		return nil
	}
	switch c := mcver.Compare(v, mcver.MustParse("1.12.2")); {
	case c > 0:
		return nil
	case c == 0 && compareBuilds(version, firstModernForge) >= 0:
		return nil
	}
	e := out.Errorf("loader-version-unsupported", "Forge %s for Minecraft %s ships the legacy installer, which shulker can't run", version, minecraft)
	e.Help = "use Minecraft 1.12.2 with Forge " + firstModernForge + " or newer"
	return e
}

// compareBuilds orders dotted build numbers like 14.23.5.2860 part by part.
func compareBuilds(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(as), len(bs)) {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			return cmp.Compare(x, y)
		}
	}
	return 0
}

func Lookup(name string) (Loader, bool) {
	for _, l := range All {
		if l.Name == name {
			return l, true
		}
	}
	return Loader{}, false
}

func ByComponentUID(uid string) (Loader, bool) {
	for _, l := range All {
		if l.ComponentUID == uid {
			return l, true
		}
	}
	return Loader{}, false
}

// ProviderLoaders are the loader names a mod may list to run on the named loader: the loader itself
// and every loader it also runs.
func ProviderLoaders(name string) []string {
	if l, ok := Lookup(name); ok {
		return append([]string{l.Name}, l.AlsoRuns...)
	}
	return []string{name}
}

func Describe(name, version string) string {
	switch {
	case name == "":
		return "no loader"
	case version == "":
		return name
	}
	return name + " " + version
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
