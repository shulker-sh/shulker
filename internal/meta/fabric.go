package meta

import (
	"context"
	"fmt"

	"github.com/andrewmast/shulker/internal/fetch"
)

const FabricMetaURL = "https://meta.fabricmc.net/v2"

type Fabric struct {
	Client  *fetch.Client
	BaseURL string
}

type LoaderVersion struct {
	Version string
	Stable  bool
}

func NewFabric(c *fetch.Client) *Fabric {
	return &Fabric{Client: c, BaseURL: FabricMetaURL}
}

func (f *Fabric) LoaderVersions(ctx context.Context, game string) ([]LoaderVersion, error) {
	var entries []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		} `json:"loader"`
	}
	if err := f.Client.GetJSON(ctx, f.BaseURL+"/versions/loader/"+game, &entries); err != nil {
		return nil, fmt.Errorf("fabric loader versions for %s: %w", game, err)
	}
	out := make([]LoaderVersion, 0, len(entries))
	for _, e := range entries {
		out = append(out, LoaderVersion{Version: e.Loader.Version, Stable: e.Loader.Stable})
	}
	return out, nil
}
