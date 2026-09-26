// Package env holds what one run of shulker reaches: the hosts it fetches from, the cache it keeps
// files in, and the sinks its progress and warnings go to. Every package that resolves, builds or
// syncs takes one, so a test hands them fakes in one place.
package env

import (
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/mojang"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/provider"
)

// Env is what one run of shulker reaches: its hosts, cache and Mojang services, and the sinks
// its log lines, progress and warnings go to.
type Env struct {
	Fetch     *fetch.Client
	Cache     *cache.Cache
	Providers provider.Providers
	// Loaders is what every loader row reaches out with.
	Loaders  *loader.Remote
	Piston   *mojang.Piston
	Runtimes *mojang.Runtimes
	Players  *player.Resolver
	// EULA is whether config.json records this user's acceptance of the Minecraft EULA, which a
	// server build writes to eula.txt. A manifest can't accept it on anyone's behalf.
	EULA bool
	// FailFast stops an install at the first download that fails, rather than trying every file
	// and failing with them all.
	FailFast bool
	// Log reports each download and install as its own step line.
	Log func(format string, args ...any)
	// Progress starts a download bar for the named files.
	Progress func(verb string, files []out.Download) *out.Progress
	// Warn reports a warning where it happens; the printer drops a repeat.
	Warn func(format string, args ...any)
	// WarnNudge is Warn with the command that deals with the warning beneath it.
	WarnNudge func(n out.Nudge, format string, args ...any)
}

// WarnEach reports each warning through Warn.
func (e *Env) WarnEach(warnings []string) {
	for _, w := range warnings {
		e.Warn("%s", w)
	}
}
