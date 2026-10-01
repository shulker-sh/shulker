package loader

import (
	"context"

	"shulker.sh/shulker/internal/lock"
)

// fabricEnsureServer locks Fabric's bundled server launcher, which fetches the loader and its
// libraries itself on first start, unlocked.
func fabricEnsureServer(ctx context.Context, row Loader, r *Remote, lk *lock.Lock) (ServerResult, error) {
	var res ServerResult
	f := newFabricMeta(r)
	l := &lk.Loader
	if l.Server == nil {
		installer, err := f.installerVersion(ctx)
		if err != nil {
			return res, err
		}
		r.log("downloading the Fabric server launcher %s", installer)
		url := f.serverJarURL(lk.Minecraft, l.Version, installer)
		sha, err := r.Cache.Fetch(ctx, r.Fetch, url)
		if err != nil {
			return res, err
		}
		l.Server = &lock.ServerJar{Installer: installer, URL: url, Sha512: sha}
		res.ChangedLock, res.WasFetched = true, true
		return res, nil
	}
	if l.Server.URL == "" {
		l.Server.URL = f.serverJarURL(lk.Minecraft, l.Version, l.Server.Installer)
		res.ChangedLock = true
	}
	if want := f.serverJarURL(lk.Minecraft, l.Version, l.Server.Installer); l.Server.URL != want {
		return res, lockedElsewhere(row, "the server launcher", l.Server.URL)
	}
	if r.Cache.Has(l.Server.Sha512) {
		return res, nil
	}
	r.log("downloading the Fabric server launcher %s", l.Server.Installer)
	if _, err := r.Cache.Ensure(ctx, r.Fetch, l.Server.URL, l.Server.Sha512); err != nil {
		return res, err
	}
	res.WasFetched = true
	return res, nil
}

// fabricVanillaServer is where Fabric's launcher keeps the vanilla jar, downloading it only when
// the build hasn't placed it there.
func fabricVanillaServer(_ Loader, minecraft string) string {
	return ".fabric/server/" + minecraft + "-server.jar"
}
