// Package env holds what one run of shulker reaches: the hosts it fetches from, the cache it keeps
// files in, and the sinks its progress and warnings go to. Every package that resolves, builds or
// syncs takes one, so a test hands them fakes in one place.
package env

import (
	"time"

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
	// MinReleaseAge is security.minReleaseAge: how long ago a provider version must have been
	// published for shulker to choose it.
	MinReleaseAge time.Duration
	// Now is the time release ages are measured to; nil is the clock.
	Now func() time.Time
	// FailFast stops an install at the first download that fails, rather than trying every file
	// and failing with them all.
	FailFast bool
	// EveryFetch keeps a step line for each fetch in a run of them, where the run would otherwise
	// draw on one live line that settles into a count.
	EveryFetch bool
	// Log reports each download and install as its own step line.
	Log func(format string, args ...any)
	// Note prints a list row about one entry where it happens, such as one kept over the file asked for.
	Note func(it out.Item)
	// Working shows work under way that clears when it ends, for a step whose outcome is its
	// own line.
	Working func(format string, args ...any)
	// Progress starts a download bar for the named files.
	Progress func(verb string, files []out.Download) *out.Progress
	// Warn reports a warning where it happens; the printer drops a repeat.
	Warn func(format string, args ...any)
	// WarnNudge is Warn with the command that deals with the warning beneath it.
	WarnNudge func(n out.Nudge, format string, args ...any)
	// WarnSecurity reports a warning one of shulker's protections raised.
	WarnSecurity func(w out.SecurityWarning)
	// WarnsRawURL is set for a command that adds or links a source, which warns that a raw
	// manifest URL brings no overrides.
	WarnsRawURL bool
}

// Clock is now's time, or the clock's when now is nil.
func Clock(now func() time.Time) time.Time {
	if now != nil {
		return now()
	}
	return time.Now()
}

// WarnEach reports each warning through Warn.
func (e *Env) WarnEach(warnings []string) {
	for _, w := range warnings {
		e.Warn("%s", w)
	}
}
