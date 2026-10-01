package loader

import (
	"context"
	"maps"
	"slices"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
)

// isServerCached reports whether a locked server's jar and every library are in the cache, each as
// what its own address gave.
func isServerCached(c *cache.Cache, s *lock.ServerJar) bool {
	if !c.HasFrom(s.Sha512, serverJarAddress(s)) {
		return false
	}
	for _, dl := range s.Libraries {
		if !c.HasFrom(dl.Sha512, dl.URL) {
			return false
		}
	}
	return true
}

// generatedServerJar is where a server jar with no url is from: shulker generated it.
const generatedServerJar = "shulker:server-launch-jar"

func serverJarAddress(s *lock.ServerJar) string {
	if s.URL == "" {
		return generatedServerJar
	}
	return s.URL
}

func ensureDownloads(ctx context.Context, r *Remote, s *lock.ServerJar) error {
	for _, name := range slices.Sorted(maps.Keys(s.Libraries)) {
		dl := s.Libraries[name]
		if _, err := r.Cache.EnsureFrom(ctx, r.Fetch, dl.URL, dl.Sha512); err != nil {
			return err
		}
	}
	return nil
}
