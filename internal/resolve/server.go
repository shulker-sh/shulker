package resolve

import (
	"context"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/mojang"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/security"
)

// EnsureServerJar caches the vanilla and loader server jars, locking any the lock doesn't have yet.
func (r *Resolver) EnsureServerJar(ctx context.Context, mt *Meta) (loader.ServerResult, error) {
	res, err := loader.Running(r.Lock).EnsureServer(ctx, mt.Loaders, r.Lock)
	if err != nil {
		return res, err
	}
	vanilla, err := r.ensureVanillaServer(ctx, mt.Piston)
	res.ChangedLock = res.ChangedLock || vanilla.ChangedLock
	res.WasFetched = res.WasFetched || vanilla.WasFetched
	return res, err
}

func (r *Resolver) ensureVanillaServer(ctx context.Context, piston *mojang.Piston) (loader.ServerResult, error) {
	var res loader.ServerResult
	if locked := r.Lock.Server; locked != nil {
		if !piston.Serves(locked.URL) {
			e := out.Errorf("provenance-mismatch", "the lock has the Minecraft server downloading from %s, which isn't Mojang's", locked.URL)
			e.Help = "remove server from the lock that names it, so shulker locks it again"
			return res, security.Refusal(security.Provenance, e)
		}

		if r.Cache.Has(locked.Sha512) {
			return res, nil
		}
		r.log("downloading the Minecraft %s server", r.Lock.Minecraft)
		if _, err := r.Cache.Ensure(ctx, r.Fetch, locked.URL, locked.Sha512); err != nil {
			return res, err
		}
		res.WasFetched = true
		return res, nil
	}
	r.log("downloading the Minecraft %s server", r.Lock.Minecraft)
	dl, err := piston.ServerDownload(ctx, r.Lock.Minecraft)
	if err != nil {
		return res, err
	}
	sha, err := r.Cache.FetchChecked(ctx, r.Fetch, dl.URL, dl.Sha1)
	if err != nil {
		return res, err
	}
	r.Lock.Server = &lock.Download{URL: dl.URL, Sha512: sha}
	res.ChangedLock, res.WasFetched = true, true
	return res, nil
}
