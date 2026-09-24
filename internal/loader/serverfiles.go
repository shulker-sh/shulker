package loader

import (
	"context"
	"maps"
	"slices"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
)

// isServerCached reports whether a locked server's jar and every library are in the cache.
func isServerCached(c *cache.Cache, s *lock.ServerJar) bool {
	if !c.Has(s.Sha512) {
		return false
	}
	for _, dl := range s.Libraries {
		if !c.Has(dl.Sha512) {
			return false
		}
	}
	return true
}

func ensureDownloads(ctx context.Context, r *Remote, s *lock.ServerJar) error {
	for _, name := range slices.Sorted(maps.Keys(s.Libraries)) {
		dl := s.Libraries[name]
		if _, err := r.Cache.Ensure(ctx, r.Fetch, dl.URL, dl.Sha512); err != nil {
			return err
		}
	}
	return nil
}
