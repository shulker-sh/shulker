package meta

import (
	"context"
	"fmt"
	"strings"

	"shulker.sh/shulker/internal/fetch"
)

const QuiltMetaURL = "https://meta.quiltmc.org/v3"

type Quilt struct {
	Client  *fetch.Client
	BaseURL string
}

func NewQuilt(c *fetch.Client) *Quilt {
	return &Quilt{Client: c, BaseURL: QuiltMetaURL}
}

// LoaderVersions lists every Quilt loader for a game. Quilt meta has no stable flag and doesn't
// sort the list; a version without a prerelease suffix counts as stable.
func (q *Quilt) LoaderVersions(ctx context.Context, game string) ([]LoaderVersion, error) {
	var entries []struct {
		Loader struct {
			Version string `json:"version"`
		} `json:"loader"`
	}
	if err := q.Client.GetJSON(ctx, q.BaseURL+"/versions/loader/"+game, &entries); err != nil {
		return nil, fmt.Errorf("quilt loader versions for %s: %w", game, err)
	}
	out := make([]LoaderVersion, 0, len(entries))
	for _, e := range entries {
		out = append(out, LoaderVersion{Version: e.Loader.Version, Stable: !strings.Contains(e.Loader.Version, "-")})
	}
	return out, nil
}
