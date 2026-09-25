package loader

import (
	"context"
	"fmt"
	"strings"

	"shulker.sh/shulker/internal/version/minecraft"
)

const ForgeMavenURL = "https://maven.minecraftforge.net"

var forge = Loader{
	Name: "forge", Title: "Forge", DependencyID: "forge",
	ComponentUID: "net.minecraftforge", MrpackKey: "forge", CurseForgeType: "1", GDLauncherType: "Forge",
	InstallServerFlag: "--installServer", InstallClientFlag: "--installClient",
	MetadataFiles: []string{"META-INF/mods.toml"}, MarkerFile: "META-INF/mods.toml",
	MarkerModLoader:        "lowcodefml",
	MinecraftJarClassifier: "bundled",
	MavenPath:              "net/minecraftforge/forge", MavenVersionPrefixesGame: true,
	versions: forgeVersions, installerURL: forgeInstallerURL,
	ensureServer: installerEnsureServer, vanillaServer: installerVanillaServer, launchArgs: installerLaunchArgs,
	onMinecraft: forgeOnMinecraft,
}

// forgeOnMinecraft is Forge before 1.13, which reads @Mod annotations and mcmod.info and has no
// marker jar, and before 1.17, whose installers set no serverJarPath or args file.
func forgeOnMinecraft(l Loader, mc minecraft.Version) Loader {
	if minecraft.Compare(mc, minecraft.MustParse("1.13")) < 0 {
		l.MetadataFiles = []string{"mcmod.info"}
		l.ModAnnotations = true
		l.MarkerFile = ""
	}
	if minecraft.Compare(mc, minecraft.MustParse("1.17")) < 0 {
		l.RootServerJars = true
		l.MinecraftJarClassifier = ""
	}
	return l
}

// forgeVersions lists the Forge builds for a game. Forge publishes them all under one artifact,
// versioned <game>-<build>, so the game version is the filter and comes back off the version.
// Every release counts as stable: the `recommended` promotion lags months behind.
func forgeVersions(ctx context.Context, r *Remote, game string) ([]Version, error) {
	var body struct {
		Versions []string `xml:"versioning>versions>version"`
	}
	if err := r.Fetch.GetXML(ctx, r.url(ForgeMavenURL)+"/net/minecraftforge/forge/maven-metadata.xml", &body); err != nil {
		return nil, fetchFailed(err, "forge", "couldn't read the Forge versions")
	}
	var versions []Version
	for _, v := range body.Versions {
		if build, ok := strings.CutPrefix(v, game+"-"); ok {
			versions = append(versions, Version{Version: build, Stable: true})
		}
	}
	return versions, nil
}

func forgeInstallerURL(r *Remote, minecraft, version string) string {
	v := minecraft + "-" + version
	return fmt.Sprintf("%s/net/minecraftforge/forge/%s/forge-%s-installer.jar", r.url(ForgeMavenURL), v, v)
}
