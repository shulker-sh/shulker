package loader

import (
	"context"
	"encoding/json"
	"fmt"
)

const FabricMetaURL = "https://meta.fabricmc.net/v2"

var fabric = Loader{
	Name: "fabric", Title: "Fabric", DependencyID: "fabricloader",
	ComponentUID: "net.fabricmc.fabric-loader", MrpackKey: "fabric-loader", CurseForgeType: "4", GDLauncherType: "Fabric",
	ServerLaunchJar: "fabric-server-launch.jar",
	MetadataFiles:   []string{"fabric.mod.json"}, MarkerFile: "fabric.mod.json",
	DependencyOverrides: "config/fabric_loader_dependencies.json",
	versions:            fabricVersions, profile: fabricProfile,
	ensureServer: fabricEnsureServer, vanillaServer: fabricVanillaServer,
}

// fabricMeta reads Fabric's meta.
type fabricMeta struct {
	r       *Remote
	baseURL string
}

func newFabricMeta(r *Remote) fabricMeta {
	return fabricMeta{r: r, baseURL: r.url(FabricMetaURL)}
}

func fabricVersions(ctx context.Context, r *Remote, game string) ([]Version, error) {
	f := newFabricMeta(r)
	var entries []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		} `json:"loader"`
	}
	if err := f.r.Fetch.GetJSON(ctx, f.baseURL+"/versions/loader/"+game, &entries); err != nil {
		return nil, fetchFailed(err, "fabric", "couldn't read the Fabric loaders for minecraft %s", game)
	}
	versions := make([]Version, 0, len(entries))
	for _, e := range entries {
		versions = append(versions, Version{Version: e.Loader.Version, Stable: e.Loader.Stable})
	}
	return versions, nil
}

func fabricProfile(ctx context.Context, r *Remote, game, version string) (json.RawMessage, error) {
	f := newFabricMeta(r)
	var raw json.RawMessage
	url := fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", f.baseURL, game, version)
	if err := f.r.Fetch.GetJSON(ctx, url, &raw); err != nil {
		return nil, fetchFailed(err, "fabric", "couldn't read the Fabric %s profile for minecraft %s", version, game)
	}
	return raw, nil
}

// installerVersion is the first stable Fabric installer listed, or the first of any when none is stable.
func (f fabricMeta) installerVersion(ctx context.Context) (string, error) {
	var entries []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := f.r.Fetch.GetJSON(ctx, f.baseURL+"/versions/installer", &entries); err != nil {
		return "", fetchFailed(err, "fabric", "couldn't read the Fabric installer versions")
	}
	for _, e := range entries {
		if e.Stable {
			return e.Version, nil
		}
	}
	if len(entries) > 0 {
		return entries[0].Version, nil
	}
	return "", invalid("Fabric's meta lists no installer versions")
}

func (f fabricMeta) serverJarURL(game, version, installer string) string {
	return fmt.Sprintf("%s/versions/loader/%s/%s/%s/server/jar", f.baseURL, game, version, installer)
}
