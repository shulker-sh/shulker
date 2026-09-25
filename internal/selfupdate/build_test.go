package selfupdate

import (
	"runtime/debug"
	"testing"
)

func buildInfo(mainVersion string, settings map[string]string) *debug.BuildInfo {
	info := &debug.BuildInfo{}
	info.Main.Version = mainVersion
	for k, v := range settings {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: k, Value: v})
	}
	return info
}

func TestDescribe(t *testing.T) {
	worktree := map[string]string{"vcs.revision": "d1556f95d232aa7f9c6f4d0b6ba8b4e5c2f0ca96", "vcs.time": "2026-09-18T22:25:43Z", "vcs.modified": "true"}
	cases := []struct {
		name          string
		version, date string
		info          *debug.BuildInfo
		want          Build
	}{
		{"release archive", "0.0.1", "2026-09-20T14:02:00Z", buildInfo("v0.0.1", nil),
			Build{Version: "0.0.1", Built: "2026-09-20T14:02:00Z", Route: Release}},
		{"release archive built in a worktree", "0.0.1", "2026-09-20T14:02:00Z", buildInfo("v0.0.1", worktree),
			Build{Version: "0.0.1", Built: "2026-09-20T14:02:00Z", Route: Release}},
		{"go install of a tag", "", "", buildInfo("v0.0.1", nil),
			Build{Version: "0.0.1", Route: GoInstall}},
		{"go install of a prerelease tag", "", "", buildInfo("v0.0.2-rc1", nil),
			Build{Version: "0.0.2-rc1", Route: GoInstall}},
		{"worktree build", "", "", buildInfo("v0.0.0-20260918222543-d1556f95d232", worktree),
			Build{Version: Dev, Commit: "d1556f95d232aa7f9c6f4d0b6ba8b4e5c2f0ca96", Modified: true, Built: "2026-09-18T22:25:43Z", Route: Source}},
		{"go install of a branch", "", "", buildInfo("v0.0.0-20260918222543-d1556f95d232", nil),
			Build{Version: Dev, Commit: "d1556f95d232"}},
		{"no vcs", "", "", buildInfo("(devel)", nil),
			Build{Version: Dev}},
		{"no build info", "", "", nil,
			Build{Version: Dev}},
	}
	for _, c := range cases {
		if got := Describe(c.version, c.date, c.info); got != c.want {
			t.Errorf("%s: Describe(%q, %q) = %+v, want %+v", c.name, c.version, c.date, got, c.want)
		}
	}
}

func TestRouteCommands(t *testing.T) {
	cases := []struct {
		route                     Route
		managed                   bool
		origin, update, uninstall string
	}{
		{Release, true, "installed from a release", "shulker self update", ""},
		{GoInstall, false, "installed with go install", "go install shulker.sh/shulker@latest", ""},
		{Source, false, "built from source", "go build .", ""},
		{"", false, "built from source", "go build .", ""},
		{Homebrew, false, "installed by Homebrew", "brew upgrade shulker", "brew uninstall shulker"},
		{Scoop, false, "installed by Scoop", "scoop update shulker", "scoop uninstall shulker"},
	}
	for _, c := range cases {
		if c.route.Managed() != c.managed || c.route.Origin() != c.origin || c.route.UpdateCommand() != c.update || c.route.UninstallCommand() != c.uninstall {
			t.Errorf("%q: managed %v, origin %q, update %q, uninstall %q", c.route, c.route.Managed(), c.route.Origin(), c.route.UpdateCommand(), c.route.UninstallCommand())
		}
	}
}
