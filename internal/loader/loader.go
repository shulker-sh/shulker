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
	Supported     bool
}

var All = []Loader{
	{Name: "fabric", DependencyID: "fabricloader", PrismUID: "net.fabricmc.fabric-loader", MrpackKey: "fabric-loader", CurseForgeType: "4", ServerLaunchJar: "fabric-server-launch.jar", MetadataFiles: []string{"fabric.mod.json"}, Supported: true},
	{Name: "quilt", DependencyID: "quilt_loader", PrismUID: "org.quiltmc.quilt-loader", MrpackKey: "quilt-loader", CurseForgeType: "5", AlsoRuns: []string{"fabric"}, ServerLaunchJar: "quilt-server-launch.jar", MetadataFiles: []string{"quilt.mod.json", "fabric.mod.json"}, Supported: true},
	{Name: "neoforge", DependencyID: "neoforge", PrismUID: "net.neoforged", MrpackKey: "neoforge", CurseForgeType: "6", InstallServerFlag: "--install-server", InstallClientFlag: "--install-client", MetadataFiles: []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"}, Supported: true},
	{Name: "forge", DependencyID: "forge", PrismUID: "net.minecraftforge", MrpackKey: "forge", CurseForgeType: "1", InstallServerFlag: "--installServer", InstallClientFlag: "--installClient", MetadataFiles: []string{"META-INF/mods.toml"}},
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

func Require(name string) (Loader, error) {
	l, ok := Lookup(name)
	if !ok || !l.Supported {
		return l, out.Errorf("unsupported-loader", "shulker doesn't support the %s loader yet", name)
	}
	return l, nil
}
