package envtest

import (
	"encoding/json"
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
	Logged     []string
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
	log := func(format string, args ...any) { e.Logged = append(e.Logged, fmt.Sprintf(format, args...)) }
	e.Env = &env.Env{
		Fetch:     f,
		Cache:     c,
		Providers: provider.Providers{"modrinth": e.Modrinth, "curseforge": e.CurseForge},
		Loaders:   &loader.Remote{Fetch: f, Cache: c, Log: log},
		Piston:    piston,
		Runtimes:  runtimes,
		Players:   player.NewResolver(profiles),
		Log:       log,
		Working:   log,
		Warn:      func(format string, args ...any) { e.Warnings = append(e.Warnings, fmt.Sprintf(format, args...)) },
		WarnNudge: func(n out.Nudge, format string, args ...any) {
			e.Warnings = append(e.Warnings, fmt.Sprintf(format, args...))
		},
	}
	FakeLoaders(t, e.CDN)
	return e
}

// FakeLoaders replaces the loader table with a fake row per loader for the rest of the test, each
// answering its versions offline. The fabric and quilt rows serve a client profile whose loader
// jar the CDN publishes, so a launch of either can be filled.
func FakeLoaders(t *testing.T, cdn *CDN) {
	t.Helper()
	fakes := []loader.Fake{
		{Name: "fabric", Versions: Versions("0.18.0-beta.1", "0.17.3", "0.17.2"), Profile: cdn.profile("fabric-loader-0.17.3-26.2", "net.fabricmc.loader.impl.launch.knot.KnotClient", "net.fabricmc:fabric-loader:0.17.3")},
		{Name: "quilt", Versions: Versions("0.20.0-beta.9", "0.30.1", "0.31.0-beta.4", "0.30.0"), Profile: cdn.profile("quilt-loader-0.30.1-26.2", "org.quiltmc.loader.impl.launch.knot.KnotClient", "org.quiltmc:quilt-loader:0.30.1")},
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

// profile is a loader's client version JSON on top of 26.2, with its one library published to the
// CDN under its maven path.
func (c *CDN) profile(id, mainClass, library string) json.RawMessage {
	parts := strings.SplitN(library, ":", 3)
	group, artifact, version := parts[0], parts[1], parts[2]
	c.Serve("/"+strings.ReplaceAll(group, ".", "/")+"/"+artifact+"/"+version+"/"+artifact+"-"+version+".jar", []byte(artifact+" "+version))
	raw, _ := json.Marshal(map[string]any{
		"id": id, "inheritsFrom": "26.2", "type": "release", "mainClass": mainClass,
		"libraries": []map[string]any{{"name": library, "url": c.URL() + "/"}},
	})
	return raw
}

// Versions is a fake row's version list; an id with a dash is a pre-release.
func Versions(ids ...string) []loader.Version {
	versions := make([]loader.Version, len(ids))
	for i, id := range ids {
		versions[i] = loader.Version{Version: id, Stable: !strings.Contains(id, "-")}
	}
	return versions
}
