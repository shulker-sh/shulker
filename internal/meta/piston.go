package meta

import (
	"context"
	"encoding/json"
	"fmt"

	"shulker.sh/shulker/internal/fetch"
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

type Download struct {
	URL  string `json:"url"`
	Sha1 string `json:"sha1"`
}

func (p *Piston) ServerDownload(ctx context.Context, game string) (Download, error) {
	m, err := p.Manifest(ctx)
	if err != nil {
		return Download{}, err
	}
	v, ok := m.Find(game)
	if !ok {
		return Download{}, fmt.Errorf("minecraft %s is not in the version list", game)
	}
	var detail struct {
		Downloads struct {
			Server Download `json:"server"`
		} `json:"downloads"`
	}
	if err := p.Client.GetJSON(ctx, v.URL, &detail); err != nil {
		return Download{}, fmt.Errorf("minecraft %s version json: %w", v.ID, err)
	}
	if detail.Downloads.Server.URL == "" || detail.Downloads.Server.Sha1 == "" {
		return Download{}, fmt.Errorf("minecraft %s has no server download", v.ID)
	}
	return detail.Downloads.Server, nil
}

// Version is a game version's own JSON, as a launcher reads it to install and start the client.
func (p *Piston) Version(ctx context.Context, game string) (json.RawMessage, error) {
	m, err := p.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	v, ok := m.Find(game)
	if !ok {
		return nil, fmt.Errorf("minecraft %s is not in the version list", game)
	}
	var detail json.RawMessage
	if err := p.Client.GetJSON(ctx, v.URL, &detail); err != nil {
		return nil, fmt.Errorf("minecraft %s version json: %w", v.ID, err)
	}
	return detail, nil
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
