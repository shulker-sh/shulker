package envtest

import (
	"fmt"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/mojang"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/provider"
)

// Env is an env.Env on fakes, with every log line and warning kept.
type Env struct {
	*env.Env
	CDN        *CDN
	Piston     *Piston
	Modrinth   *Host
	CurseForge *Host
	Log        []string
	Warnings   []string
}

// New is an env on a temp cache, a CDN with modrinth and curseforge hosts publishing to it, a
// fake Piston, and the loader table replaced by fake rows for fabric 0.17.3, quilt 0.30.1,
// neoforge 26.2.0.87 and forge 65.1.3 for the rest of the test.
func New(t *testing.T) *Env {
	t.Helper()
	e := &Env{CDN: NewCDN(t), Piston: NewPiston(t)}
	e.Modrinth = NewHost(e.CDN, "modrinth")
	e.CurseForge = NewHost(e.CDN, "curseforge").LikeCurseForge()
	e.CurseForge.Label = "CurseForge"
	f := fetch.New("test")
	c := &cache.Cache{Dir: t.TempDir()}
	piston, runtimes, profiles := mojang.NewPiston(f), mojang.NewRuntimes(f), mojang.NewProfiles(f)
	e.Piston.Mojang(piston, runtimes, profiles)
	log := func(format string, args ...any) { e.Log = append(e.Log, fmt.Sprintf(format, args...)) }
	e.Env = &env.Env{
		Fetch:     f,
		Cache:     c,
		Providers: provider.Providers{"modrinth": e.Modrinth, "curseforge": e.CurseForge},
		Loaders:   &loader.Remote{Fetch: f, Cache: c, Log: log},
		Piston:    piston,
		Runtimes:  runtimes,
		Players:   player.NewResolver(profiles),
		Log:       log,
		Warn:      func(format string, args ...any) { e.Warnings = append(e.Warnings, fmt.Sprintf(format, args...)) },
		WarnNudge: func(n out.Nudge, format string, args ...any) {
			e.Warnings = append(e.Warnings, fmt.Sprintf(format, args...))
		},
	}
	FakeLoaders(t)
	return e
}

// FakeLoaders replaces the loader table with a fake row per loader for the rest of the test, each
// answering its versions offline.
func FakeLoaders(t *testing.T) {
	t.Helper()
	fakes := []loader.Fake{
		{Name: "fabric", Versions: Versions("0.18.0-beta.1", "0.17.3", "0.17.2")},
		{Name: "quilt", Versions: Versions("0.20.0-beta.9", "0.30.1", "0.31.0-beta.4", "0.30.0")},
		{Name: "neoforge", Versions: Versions("26.2.0.56-beta", "26.2.0.87")},
		{Name: "forge", Versions: Versions("65.0.9", "65.1.3")},
	}
	rows := make([]loader.Loader, len(fakes))
	for i, f := range fakes {
		rows[i] = f.Row()
	}
	real := loader.All
	loader.All = rows
	t.Cleanup(func() { loader.All = real })
}

// Versions is a fake row's version list; an id with a dash is a pre-release.
func Versions(ids ...string) []loader.Version {
	versions := make([]loader.Version, len(ids))
	for i, id := range ids {
		versions[i] = loader.Version{Version: id, Stable: !strings.Contains(id, "-")}
	}
	return versions
}
