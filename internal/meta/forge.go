package meta

import (
	"context"
	"fmt"
	"strings"

	"shulker.sh/shulker/internal/fetch"
)

const ForgeMavenURL = "https://maven.minecraftforge.net"

type Forge struct {
	Client  *fetch.Client
	BaseURL string
}

func NewForge(c *fetch.Client) *Forge {
	return &Forge{Client: c, BaseURL: ForgeMavenURL}
}

// LoaderVersions lists the Forge builds for a game. Forge publishes them all under one artifact,
// versioned <game>-<build>, so the game version is the filter and comes back off the version.
// Every release counts as stable: the `recommended` promotion lags months behind.
func (f *Forge) LoaderVersions(ctx context.Context, game string) ([]LoaderVersion, error) {
	var body struct {
		Versions []string `xml:"versioning>versions>version"`
	}
	if err := f.Client.GetXML(ctx, f.BaseURL+"/net/minecraftforge/forge/maven-metadata.xml", &body); err != nil {
		return nil, fmt.Errorf("forge versions: %w", err)
	}
	var out []LoaderVersion
	for _, v := range body.Versions {
		if build, ok := strings.CutPrefix(v, game+"-"); ok {
			out = append(out, LoaderVersion{Version: build, Stable: true})
		}
	}
	return out, nil
}

func (f *Forge) InstallerURL(minecraft, version string) string {
	v := minecraft + "-" + version
	return fmt.Sprintf("%s/net/minecraftforge/forge/%s/forge-%s-installer.jar", f.BaseURL, v, v)
}
