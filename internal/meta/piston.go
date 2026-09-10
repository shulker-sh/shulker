package meta

import (
	"context"
	"fmt"

	"github.com/andrewmast/shulker/internal/fetch"
)

const PistonManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type Piston struct {
	Client      *fetch.Client
	ManifestURL string
}

type GameVersion struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	ReleaseTime string `json:"releaseTime"`
}

type GameManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []GameVersion `json:"versions"`
}

type JavaRuntime struct {
	Component string `json:"component"`
	Major     int    `json:"majorVersion"`
}

func NewPiston(c *fetch.Client) *Piston {
	return &Piston{Client: c, ManifestURL: PistonManifestURL}
}

func (p *Piston) Manifest(ctx context.Context) (*GameManifest, error) {
	var m GameManifest
	if err := p.Client.GetJSON(ctx, p.ManifestURL, &m); err != nil {
		return nil, fmt.Errorf("minecraft version list: %w", err)
	}
	return &m, nil
}

func (m *GameManifest) Find(id string) (GameVersion, bool) {
	for _, v := range m.Versions {
		if v.ID == id {
			return v, true
		}
	}
	return GameVersion{}, false
}

func (p *Piston) Java(ctx context.Context, v GameVersion) (JavaRuntime, error) {
	var detail struct {
		JavaVersion JavaRuntime `json:"javaVersion"`
	}
	if err := p.Client.GetJSON(ctx, v.URL, &detail); err != nil {
		return JavaRuntime{}, fmt.Errorf("minecraft %s version json: %w", v.ID, err)
	}
	if detail.JavaVersion.Component == "" {
		return JavaRuntime{}, fmt.Errorf("minecraft %s version json has no javaVersion", v.ID)
	}
	return detail.JavaVersion, nil
}
