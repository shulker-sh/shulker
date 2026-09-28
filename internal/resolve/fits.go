package resolve

import (
	"context"
	"slices"

	"shulker.sh/shulker/internal/provider"
)

// Fits reports whether projectID on the named provider has a version, on any channel, for the
// project's Minecraft and loader as kind needs them.
func (r *Resolver) Fits(ctx context.Context, providerName, projectID, kind string) (bool, error) {
	p, err := r.Providers.Get(providerName)
	if err != nil {
		return false, err
	}
	q := r.queryFor(kind, providerName)
	tags := q.tags
	if q.untagged {
		tags = nil
	}
	versions, err := p.Versions(ctx, projectID, q.game, tags)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(versions, func(v provider.Version) bool {
		return !q.untagged || len(q.tags) == 0 || len(v.Loaders) == 0 || slices.ContainsFunc(v.Loaders, func(l string) bool { return slices.Contains(q.tags, l) })
	}), nil
}
