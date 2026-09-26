package sync

import (
	"context"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/java"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// ServerJavaFix is the fix row a server's runtime-unavailable error carries.
var ServerJavaFix = out.Detail{Label: "Fix", Text: `set "java" in shulker.json to a JDK path`}

// LinkJavaFix is the fix row for a client runtime a launcher can't get: relink with a Java.
func LinkJavaFix(launcherName string) out.Detail {
	return out.Detail{Label: "Fix", Text: "shulker link " + launcherName + " --java <path>", IsCommand: true}
}

// RuntimeWarning is a runtime error as one warning line: its message, and its fix when it has one.
func RuntimeWarning(err error) string {
	e := out.AsError(err)
	if len(e.Rows) == 0 {
		return out.Sentence(e.Message)
	}
	return out.Sentence(e.Message) + "; " + e.Rows[0].Text
}

// ManagedJava ensures the lock's runtime component. fix is the Fix row a runtime-unavailable
// error carries, which depends on which side needs the Java.
func ManagedJava(ctx context.Context, e *Env, p *project.Project, refresh bool, fix out.Detail) (java.Runtime, error) {
	opts := java.RuntimeOptions{Refresh: refresh, Log: e.Log}
	rt, err := java.EnsureRuntime(ctx, e.Fetch, e.Runtimes, e.Cache.Dir, p.Lock.Java.Component, opts)
	switch out.CodeOf(err) {
	case "runtime-unavailable":
		out.AsError(err).Rows = []out.Detail{fix}
	case "rosetta-required":
		fail := out.AsError(err)
		fail.Rows = append(fail.Rows, fix)
	}
	return rt, err
}

// FreshestJava ensures the lock's runtime component against Mojang's current manifest, keeping
// the installed copy when the network is away.
func FreshestJava(ctx context.Context, e *Env, p *project.Project, fix out.Detail) (java.Runtime, error) {
	rt, err := ManagedJava(ctx, e, p, true, fix)
	if err != nil && fetch.IsNetwork(err) {
		if kept, keptErr := ManagedJava(ctx, e, p, false, fix); keptErr == nil {
			e.Warn("offline, keeping the installed Java runtime %s %s", kept.Component, kept.Version)
			return kept, nil
		}
	}
	return rt, err
}

// ProjectJava is the Java a project's server runs on: the manifest's, else the managed runtime,
// else whatever java is on PATH.
func ProjectJava(ctx context.Context, e *Env, p *project.Project) (java.Binary, error) {
	if p.Manifest.Java != "" {
		return java.Find(p.Manifest.Java, p.Lock.Java.Major)
	}
	rt, err := ManagedJava(ctx, e, p, false, ServerJavaFix)
	if err != nil {
		if out.CodeOf(err) != "runtime-unavailable" {
			return java.Binary{}, err
		}
		e.Warn("%s; using java on PATH", RuntimeWarning(err))
		return java.Find("", p.Lock.Java.Major)
	}
	return java.At(rt.Home)
}

// recordClientRuntime is what a Mojang profile launches with: the shim runs shulker's managed
// runtime, never the launcher's, so a sync of a client linked there ensures the lock's component
// and writes its java into resolved.java. Other launchers bring their own Java, and a `java`
// setting stands in for the download.
func (e *Env) recordClientRuntime(ctx context.Context, p *project.Project, side, dir string) (java.Runtime, error) {
	if side != "client" {
		return java.Runtime{}, nil
	}
	in, ok := e.registered(dir)
	if !ok {
		return java.Runtime{}, nil
	}
	if l := launcher.Find(in.Launcher); l == nil || !l.NeedsRuntime {
		return java.Runtime{}, nil
	}
	f, err := instance.Load(dir)
	if err != nil {
		return java.Runtime{}, err
	}
	var rt java.Runtime
	bin := f.Settings.Java
	if bin == "" {
		if rt, err = FreshestJava(ctx, e, p, LinkJavaFix(in.Launcher)); err != nil {
			return java.Runtime{}, err
		}
		bin = java.Bin(rt.Home)
	}
	f.EnsureResolved().Java = bin
	return rt, f.Save(dir)
}
