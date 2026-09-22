package meta

import (
	"context"

	"shulker.sh/shulker/internal/fetch"
)

const GDLauncherMetaURL = "https://meta.gdl.gg"

// gdlauncherAnyGame is the game id GDLauncher's meta lists Fabric and Quilt loaders under, since one
// loader build runs on every game version.
const gdlauncherAnyGame = "${gdlauncher.gameVersion}"

// GDLauncher reads GDLauncher's own meta, the only place GDLauncher installs loaders from.
type GDLauncher struct {
	Client  *fetch.Client
	BaseURL string
}

func NewGDLauncher(c *fetch.Client) *GDLauncher {
	return &GDLauncher{Client: c, BaseURL: GDLauncherMetaURL}
}

// LoaderVersions lists the versions of a loader GDLauncher can install for a game, named the way its
// meta names them.
func (g *GDLauncher) LoaderVersions(ctx context.Context, loader, game string) ([]string, error) {
	var manifest struct {
		GameVersions []struct {
			ID      string `json:"id"`
			Loaders []struct {
				ID string `json:"id"`
			} `json:"loaders"`
		} `json:"gameVersions"`
	}
	if err := g.Client.GetJSON(ctx, g.BaseURL+"/"+loader+"/v2/manifest.json", &manifest); err != nil {
		return nil, fetchFailed(err, "gdlauncher", "couldn't read GDLauncher's %s versions", loader)
	}
	var versions []string
	for _, gv := range manifest.GameVersions {
		if gv.ID != game && gv.ID != gdlauncherAnyGame {
			continue
		}
		for _, l := range gv.Loaders {
			versions = append(versions, l.ID)
		}
	}
	return versions, nil
}
