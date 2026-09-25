package cli

import (
	"context"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/java"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// freshestJava ensures the lock's runtime component against Mojang's current manifest, keeping the
// installed copy when the network is away.
func (a *app) freshestJava(ctx context.Context, p *project.Project, fix out.Detail) (java.Runtime, error) {
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
func (a *app) recordClientRuntime(ctx context.Context, p *project.Project, side, dir string) (java.Runtime, error) {
	if side != "client" {
		return java.Runtime{}, nil
	}
	in, ok := a.registeredInstance(dir)
	if !ok {
		return java.Runtime{}, nil
	}
	if e := launcher.Find(in.Launcher); e == nil || !e.NeedsRuntime {
		return java.Runtime{}, nil
	}
	f, err := instance.Load(dir)
	if err != nil {
		return java.Runtime{}, err
	}
	var rt java.Runtime
	bin := f.Settings.Java
	if bin == "" {
		if rt, err = a.freshestJava(ctx, p, linkJavaFix(in.Launcher)); err != nil {
			return java.Runtime{}, err
		}
		bin = java.Bin(rt.Home)
	}
	f.EnsureResolved().Java = bin
	return rt, f.Save(dir)
}
