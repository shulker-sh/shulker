package cli

import (
	"context"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/server"
)

// freshestJava ensures the lock's runtime component against Mojang's current manifest, keeping the
// installed copy when the network is away.
func (a *app) freshestJava(ctx context.Context, p *project.Project, fix out.Detail) (server.Runtime, error) {
	rt, err := a.managedJava(ctx, p, true, fix)
	if err != nil && fetch.IsNetwork(err) {
		a.printer.Drop()
		if kept, keptErr := a.managedJava(ctx, p, false, fix); keptErr == nil {
			a.printer.Warn("offline, keeping the installed Java runtime %s %s", kept.Component, kept.Version)
			return kept, nil
		}
	}
	return rt, err
}

// recordClientRuntime is what a Mojang profile launches with: the shim runs shulker's managed
// runtime, never the launcher's, so a sync of a client linked there ensures the lock's component and
// writes its java into resolved.java. Other launchers bring their own Java, and a `java` setting
// stands in for the download.
func (a *app) recordClientRuntime(ctx context.Context, p *project.Project, side, dir string) (server.Runtime, error) {
	if side != "client" {
		return server.Runtime{}, nil
	}
	in, ok := a.registeredInstance(dir)
	if !ok {
		return server.Runtime{}, nil
	}
	if e := launcher.Find(in.Launcher); e == nil || !e.NeedsRuntime {
		return server.Runtime{}, nil
	}
	f, err := instance.Load(dir)
	if err != nil {
		return server.Runtime{}, err
	}
	var rt server.Runtime
	java := f.Settings.Java
	if java == "" {
		if rt, err = a.freshestJava(ctx, p, linkJavaFix(in.Launcher)); err != nil {
			return server.Runtime{}, err
		}
		java = server.JavaBin(rt.Home)
	}
	f.EnsureResolved().Java = java
	return rt, f.Save(dir)
}
