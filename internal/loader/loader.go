// Package loader is the one table of what shulker knows about each mod loader, and of what each
// one does: reads its versions, serves its client profile, and sets up a server or a client.
package loader

import (
	"cmp"
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/version/minecraft"
)

// VanillaServerFile is the vanilla server jar's name in a server dir that runs it directly.
const VanillaServerFile = "server.jar"

// Version is one of the versions a loader publishes for a Minecraft version.
type Version struct {
	Version string
	Stable  bool
}

// Remote is what a row reaches out with: the fetch client, the cache every download lands in,
// and the seams the CLI hands it for reporting and for running an installer.
type Remote struct {
	Fetch *fetch.Client
	Cache *cache.Cache
	// URLs replaces a service's base URL, keyed by the one the row would use, so a test can point
	// it at a fake.
	URLs map[string]string
	// Log reports each download and install as its own step line.
	Log func(format string, args ...any)
	// RunInstaller runs a loader's own installer jar with the given Java.
	RunInstaller func(ctx context.Context, java, jar string, args []string) error
}

func (r *Remote) url(base string) string {
	if replaced, ok := r.URLs[base]; ok {
		return replaced
	}
	return base
}

func (r *Remote) log(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
	}
}

// ServerResult says what ensuring a server's files did.
type ServerResult struct {
	ChangedLock bool
	WasFetched  bool
}

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
	// CurseForgeGameInID lists the Minecraft versions whose loader id in a CurseForge pack manifest
	// carries the game version too, as neoforge-1.20.1-<v>: those builds kept Forge's numbering,
	// and the game version is how CurseForge tells them apart.
	CurseForgeGameInID []string
	// GDLauncherType is the loader's name in a GDLauncher instance's modloaders list.
	GDLauncherType string
	// AlsoRuns are the loaders whose mods this one runs as well.
	AlsoRuns []string
	// ServerLaunchJar is the jar a server built for this loader starts from, for a loader whose
	// server shulker assembles rather than installs.
	ServerLaunchJar string
	// InstallServerFlag is the flag the loader's own installer takes, with the server dir, to set up
	// a server. Empty means shulker assembles the server itself.
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
	// MarkerModLoader is the modLoader a TOML marker declares, with the loaderVersion that goes
	// with it. Empty leaves both keys out, for a loader that rejects a loaderVersion without a
	// modLoader and defaults a missing modLoader itself: NeoForge from 1.21.5 on.
	MarkerModLoader string
	// CompatibleVersions are the versions, by dependency id, that a Maven range which rejects the
	// running minecraft or loader version may accept instead: FML's VersionSupportMatrix, for a
	// Minecraft patch release that keeps its mods compatible with the one before.
	CompatibleVersions map[string][]string
	// MarkerIconFile means the TOML marker names its logo as iconFile too, for a loader that shows
	// the square icon beside the name only from that key.
	MarkerIconFile bool
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

	// versions lists what the loader publishes for a Minecraft version.
	versions func(ctx context.Context, r *Remote, game string) ([]Version, error)
	// profile is the launcher profile JSON the loader's meta serves, for a loader a launcher installs
	// from one rather than with an installer.
	profile func(ctx context.Context, r *Remote, game, version string) (json.RawMessage, error)
	// providesJar is the URL of the loader's own jar, for a loader whose jar declares what it
	// provides in place of another loader.
	providesJar func(ctx context.Context, r *Remote, game, version string) (string, error)
	// installerURL is where the loader's own installer jar is published.
	installerURL func(r *Remote, minecraft, version string) string
	// ensureServer caches the loader's server files, locking any the lock doesn't have yet.
	ensureServer func(ctx context.Context, l Loader, r *Remote, lk *lock.Lock) (ServerResult, error)
	// vanillaServer is where the loader looks for the vanilla server jar, relative to the server dir.
	vanillaServer func(l Loader, minecraft string) string
	// launchArgs start the server from its dir, for a loader that doesn't start from ServerLaunchJar.
	launchArgs func(l Loader, lk *lock.Lock) []string
	// onMinecraft is the row as it runs on one Minecraft version, for a loader whose older
	// generations differ.
	onMinecraft func(l Loader, mc minecraft.Version) Loader
}

// supportMatrix is the row of an FML VersionSupportMatrix for the running Minecraft version. The
// rows are read from each FML branch; see docs/research/fml-version-support-matrix.md.
func supportMatrix(mc minecraft.Version, rows map[string]map[string][]string) map[string][]string {
	for game, row := range rows {
		if minecraft.Compare(mc, minecraft.MustParse(game)) == 0 {
			return row
		}
	}
	return nil
}

// ArtifactVersion is the version the loader publishes its own jars under.
func (l Loader) ArtifactVersion(minecraft, version string) string {
	if l.MavenVersionPrefixesGame {
		return minecraft + "-" + version
	}
	return version
}

var All = []Loader{fabric, quilt, neoforge, forge}

// For is the named loader as it runs on the given Minecraft version, for a loader whose older
// generations differ.
func For(name, mc string) (Loader, bool) {
	l, ok := Lookup(name)
	if !ok || l.onMinecraft == nil {
		return l, ok
	}
	v, err := minecraft.Parse(mc)
	if err != nil {
		return l, ok
	}
	return l.onMinecraft(l, v), ok
}

// Running is the locked loader as it runs on the locked Minecraft version, or the zero Loader for
// a project without one.
func Running(lk *lock.Lock) Loader {
	l, _ := For(lk.Loader.Type, lk.Minecraft)
	return l
}

// VanillaServerJar is the name a RootServerJars installer looks for the vanilla server jar under.
func VanillaServerJar(minecraft string) string {
	return "minecraft_server." + minecraft + ".jar"
}

// InstalledServerJar is the jar a RootServerJars installer leaves in the server dir to start from.
func (l Loader) InstalledServerJar(minecraft, version string) string {
	return l.Name + "-" + l.ArtifactVersion(minecraft, version) + ".jar"
}

// Versions lists the loader's versions for a Minecraft version.
func (l Loader) Versions(ctx context.Context, r *Remote, game string) ([]Version, error) {
	if l.versions == nil {
		return nil, out.Errorf("unsupported-loader", "shulker has no version list for the %s loader", l.Name)
	}
	return l.versions(ctx, r, game)
}

// HasInstaller reports whether a client gets the loader by running its own installer rather than
// from a launcher profile its meta serves.
func (l Loader) HasInstaller() bool { return l.InstallClientFlag != "" }

// Profile is the launcher profile JSON the loader's meta serves for a version on a Minecraft version.
func (l Loader) Profile(ctx context.Context, r *Remote, game, version string) (json.RawMessage, error) {
	if l.profile == nil {
		return nil, out.Errorf("unsupported-loader", "shulker has no launcher profile for the %s loader", l.Name)
	}
	return l.profile(ctx, r, game, version)
}

// ProvidesJar is the URL of the loader's own jar when its metadata says what the loader provides
// in place of another loader, which the lock records so validation stays offline.
func (l Loader) ProvidesJar(ctx context.Context, r *Remote, game, version string) (string, bool, error) {
	if l.providesJar == nil {
		return "", false, nil
	}
	url, err := l.providesJar(ctx, r, game, version)
	return url, err == nil, err
}

// EnsureServer caches the loader's server files, locking any the lock doesn't have yet.
func (l Loader) EnsureServer(ctx context.Context, r *Remote, lk *lock.Lock) (ServerResult, error) {
	if l.ensureServer == nil {
		return ServerResult{}, nil
	}
	return l.ensureServer(ctx, l, r, lk)
}

// VanillaServerPath is where the loader looks for the vanilla server jar, relative to the server
// dir; a project without a loader runs it from there.
func (l Loader) VanillaServerPath(minecraft string) string {
	if l.vanillaServer == nil {
		return VanillaServerFile
	}
	return l.vanillaServer(l, minecraft)
}

// LaunchArgs start the server from its dir: the launch jar, the file the loader's installer left,
// or the vanilla jar when there is no loader.
func (l Loader) LaunchArgs(lk *lock.Lock) []string {
	if l.launchArgs != nil {
		return l.launchArgs(l, lk)
	}
	if l.ServerLaunchJar != "" {
		return []string{"-jar", l.ServerLaunchJar}
	}
	return []string{"-jar", VanillaServerFile}
}

// firstModernForge is the first Forge build whose installer shulker can run offline: every earlier
// one ships the legacy installer, which reads another install profile layout.
const firstModernForge = "14.23.5.2851"

// Supports reports whether shulker can set up the named loader's version on the given Minecraft
// version.
func Supports(name, mc, version string) error {
	if name != forge.Name {
		return nil
	}
	v, err := minecraft.Parse(mc)
	if err != nil {
		return nil
	}
	switch c := minecraft.Compare(v, minecraft.MustParse("1.12.2")); {
	case c > 0:
		return nil
	case c == 0 && compareBuilds(version, firstModernForge) >= 0:
		return nil
	}
	e := out.Errorf("loader-version-unsupported", "Forge %s for Minecraft %s ships the legacy installer, which shulker can't run", version, mc)
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

// Title is the name a player reads for the loader called name, or name itself for one shulker
// doesn't know.
func Title(name string) string {
	if l, ok := Lookup(name); ok {
		return l.Title
	}
	return name
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
