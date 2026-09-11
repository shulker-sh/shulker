// Package loader is the one table of what shulker knows about each mod loader.
package loader

import "shulker.sh/shulker/internal/out"

type Loader struct {
	Name string
	// DependencyID is how a mod's metadata names the loader in its dependencies.
	DependencyID   string
	PrismUID       string
	MrpackKey      string
	CurseForgeType string
	Supported      bool
}

var All = []Loader{
	{Name: "fabric", DependencyID: "fabricloader", PrismUID: "net.fabricmc.fabric-loader", MrpackKey: "fabric-loader", CurseForgeType: "4", Supported: true},
	{Name: "quilt", DependencyID: "quilt_loader", PrismUID: "org.quiltmc.quilt-loader", MrpackKey: "quilt-loader", CurseForgeType: "5"},
	{Name: "neoforge", DependencyID: "neoforge", PrismUID: "net.neoforged", MrpackKey: "neoforge", CurseForgeType: "6"},
	{Name: "forge", DependencyID: "forge", PrismUID: "net.minecraftforge", MrpackKey: "forge", CurseForgeType: "1"},
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
