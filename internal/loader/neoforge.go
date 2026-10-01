package loader

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/version/minecraft"
)

const NeoForgeMavenURL = "https://maven.neoforged.net"

var neoforge = Loader{
	Name: "neoforge", Title: "NeoForge", DependencyID: "neoforge",
	ComponentUID: "net.neoforged", MrpackKey: "neoforge", CurseForgeType: "6", GDLauncherType: "Neoforge",
	CurseForgeGameInID: []string{"1.20.1"},
	InstallServerFlag:  "--install-server", InstallClientFlag: "--install-client",
	MetadataFiles: []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"}, MarkerFile: "META-INF/neoforge.mods.toml",
	MarkerIconFile: true,
	MavenPath:      "net/neoforged/neoforge",
	versions:       neoforgeVersions, installerURL: neoforgeInstallerURL,
	ensureServer: installerEnsureServer, vanillaServer: installerVanillaServer, launchArgs: installerLaunchArgs,
	onMinecraft: neoforgeOnMinecraft,
}

// neoforgeOnMinecraft is NeoForge before 1.21.5, whose FML rejects a mod file that names no
// modLoader and loaderVersion (from 1.21.5 on both are optional, and FML warns that lowcodefml is
// deprecated), NeoForge from 1.21.1 on, whose FML reads dependency overrides from its own config,
// and the rows of FML's VersionSupportMatrix.
func neoforgeOnMinecraft(l Loader, mc minecraft.Version) Loader {
	if minecraft.Compare(mc, minecraft.MustParse("1.21.5")) < 0 {
		l.MarkerModLoader = "lowcodefml"
	}
	if minecraft.Compare(mc, minecraft.MustParse("1.21.1")) >= 0 {
		l.DependencyOverrides = "config/fml.toml"
	}
	l.CompatibleVersions = supportMatrix(mc, map[string]map[string][]string{
		"1.21.1": {"minecraft": {"1.21"}, "neoforge": {"21.0.166"}},
		"1.21.8": {"minecraft": {"1.21.7"}, "neoforge": {"21.7.26-beta"}},
	})
	return l
}

// neoforgeVersions lists the NeoForge builds for a game. NeoForge numbers its builds after the game
// (1.21.1 → 21.1.x, 26.2 → 26.2.0.x); early builds carry -beta and snapshot builds -alpha.
func neoforgeVersions(ctx context.Context, r *Remote, game string) ([]Version, error) {
	prefix, ok := neoforgePrefix(game)
	if !ok {
		return nil, nil
	}
	var body struct {
		Versions []string `json:"versions"`
	}
	if err := r.Fetch.GetJSON(ctx, r.url(NeoForgeMavenURL)+"/api/maven/versions/releases/net/neoforged/neoforge", &body); err != nil {
		return nil, fetchFailed(err, "neoforge", "couldn't read the NeoForge versions")
	}
	var versions []Version
	for _, v := range body.Versions {
		if strings.HasPrefix(v, prefix) {
			versions = append(versions, Version{Version: v, Stable: !strings.Contains(v, "-")})
		}
	}
	return versions, nil
}

func neoforgeInstallerURL(r *Remote, _, version string) string {
	return fmt.Sprintf("%s/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", r.url(NeoForgeMavenURL), version, version)
}

func neoforgePrefix(game string) (string, bool) {
	parts := strings.Split(game, ".")
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return "", false
		}
	}
	width := 3
	if parts[0] == "1" {
		parts, width = parts[1:], 2
	}
	if len(parts) < width-1 || len(parts) > width {
		return "", false
	}
	if len(parts) < width {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".") + ".", true
}
