package meta

import (
	"context"
	"encoding/json"
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

func (f *Fabric) LoaderProfile(ctx context.Context, game, loader string) (json.RawMessage, error) {
	var raw json.RawMessage
	url := fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", f.BaseURL, game, loader)
	if err := f.Client.GetJSON(ctx, url, &raw); err != nil {
		return nil, fmt.Errorf("fabric profile for %s with loader %s: %w", game, loader, err)
	}
	return raw, nil
}
