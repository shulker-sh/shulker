package resolve

import (
	"context"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/meta"
)

type ServerJarResult struct {
	Locked  bool
	Fetched bool
}

func (r *Resolver) EnsureServerJar(ctx context.Context, fabric *meta.Fabric) (ServerJarResult, error) {
	var res ServerJarResult
	l := &r.Lock.Loader
	if _, err := loader.Require(l.Type); err != nil {
		return res, err
	}
	if l.Server == nil {
		installer, err := fabric.InstallerVersion(ctx)
		if err != nil {
			return res, err
		}
		r.log("downloading fabric server launcher (installer %s)", installer)
		sha, err := r.Cache.Fetch(ctx, r.Fetch, fabric.ServerJarURL(r.Lock.Minecraft, l.Version, installer))
		if err != nil {
			return res, err
		}
		l.Server = &lock.ServerJar{Installer: installer, Sha512: sha}
		res.Locked, res.Fetched = true, true
		return res, nil
	}
	if r.Cache.Has(l.Server.Sha512) {
		return res, nil
	}
	r.log("downloading fabric server launcher (installer %s)", l.Server.Installer)
	if _, err := r.Cache.Ensure(ctx, r.Fetch, fabric.ServerJarURL(r.Lock.Minecraft, l.Version, l.Server.Installer), l.Server.Sha512); err != nil {
		return res, err
	}
	res.Fetched = true
	return res, nil
}
