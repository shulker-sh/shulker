package sync

import (
	"context"
	"time"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/takedown"
)

// takedownInterval is how long a sync goes on the last takedown check before asking the providers
// again.
const takedownInterval = 24 * time.Hour

// checkTakedowns asks the providers about every file l locks, unless dir's last check is less than
// a day old. Offline, or when no provider could be asked, it records nothing, so the next sync
// tries again; nil keeps the last check.
func (e *Env) checkTakedowns(ctx context.Context, dir string, l *lock.Lock, offline bool) *instance.Takedowns {
	if offline || e.Fetch != nil && e.Fetch.Offline {
		return nil
	}
	now := env.Clock(e.Now)
	if last := instance.LoadState(dir).Takedowns; last != nil {
		if at, err := time.Parse(time.RFC3339, last.CheckedAt); err == nil && now.Sub(at) < takedownInterval {
			return nil
		}
	}
	res := takedown.Check(ctx, e.Providers, e.Cache, takedown.Entries(l))
	if len(res.Skipped) > 0 && len(res.With(takedown.Unchecked)) == len(res.Files) {
		return nil
	}
	return &instance.Takedowns{CheckedAt: now.UTC().Format(time.RFC3339), Files: append(res.With(takedown.Gone), res.With(takedown.Moved)...)}
}

// usedBy names the registered instances whose lock holds each file.
func (e *Env) usedBy(sha512s []string) map[string][]string {
	instances, err := e.instances()
	if err != nil {
		return nil
	}
	entries := make([]project.InstanceEntry, len(instances))
	for i, in := range instances {
		entries[i] = project.Inspect(in)
	}
	return build.InstancesUsing(e.Cache, entries, sha512s)
}

// warnContext is what the warnings about a build of p into dir name.
func (e *Env) warnContext(p *project.Project, side, dir string) build.WarnContext {
	c := build.WarnContext{Providers: e.Providers, UsedBy: e.usedBy}
	if e.Rebuild != nil {
		c.Force = e.Rebuild(p, side, dir)
	}
	return c
}
