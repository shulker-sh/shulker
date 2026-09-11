package meta

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/fetch"
)

const NeoForgeMavenURL = "https://maven.neoforged.net"

type NeoForge struct {
	Client  *fetch.Client
	BaseURL string
}

func NewNeoForge(c *fetch.Client) *NeoForge {
	return &NeoForge{Client: c, BaseURL: NeoForgeMavenURL}
}

// LoaderVersions lists the NeoForge builds for a game. NeoForge numbers its builds after the game
// (1.21.1 → 21.1.x, 26.2 → 26.2.0.x); early builds carry -beta and snapshot builds -alpha.
func (n *NeoForge) LoaderVersions(ctx context.Context, game string) ([]LoaderVersion, error) {
	prefix, ok := neoForgePrefix(game)
	if !ok {
		return nil, nil
	}
	var body struct {
		Versions []string `json:"versions"`
	}
	if err := n.Client.GetJSON(ctx, n.BaseURL+"/api/maven/versions/releases/net/neoforged/neoforge", &body); err != nil {
		return nil, fmt.Errorf("neoforge versions: %w", err)
	}
	var out []LoaderVersion
	for _, v := range body.Versions {
		if strings.HasPrefix(v, prefix) {
			out = append(out, LoaderVersion{Version: v, Stable: !strings.Contains(v, "-")})
		}
	}
	return out, nil
}

func neoForgePrefix(game string) (string, bool) {
	parts := strings.Split(game, ".")
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return "", false
		}
	}
	width := 3
	if parts[0] == "1" {
		parts, width = parts[1:], 2
	}
	if len(parts) < width-1 || len(parts) > width {
		return "", false
	}
	if len(parts) < width {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".") + ".", true
}
