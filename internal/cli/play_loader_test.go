package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlayLaunchesAModdedInstance(t *testing.T) {
	for _, tc := range []struct {
		loader, version, mainClass, library string
		libraries                           int
	}{
		{"fabric", "fabric-loader-0.17.3-26.2", "net.fabricmc.loader.impl.launch.knot.KnotClient", "net/fabricmc/fabric-loader/0.17.3/fabric-loader-0.17.3.jar", 1},
		{"quilt", "quilt-loader-0.30.1-26.2", "org.quiltmc.loader.impl.launch.knot.KnotClient", "org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar", 1},
		{"forge", "26.2-forge-65.1.3", "net.minecraftforge.bootstrap.ForgeBootstrap", "net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-client.jar", 2},
		{"neoforge", "neoforge-26.2.0.87", "cpw.mods.bootstraplauncher.BootstrapLauncher", "net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-universal.jar", 2},
	} {
		t.Run(tc.loader, func(t *testing.T) {
			h := newHarness(t)
			store, gameDir := playHarness(t, h, "--loader", tc.loader)
			h.mustRun(t, "accounts", "login", "--use")

			rep := playJSON(t, h, "-i", "pack", "play", "--dry-run")
			if rep.Version != tc.version || rep.Inherits != "26.2" || rep.MainClass != tc.mainClass {
				t.Fatalf("report %+v", rep)
			}
			if rep.LoaderLibraries != tc.libraries || rep.LoaderLibrariesBytes == 0 {
				t.Fatalf("loader libraries %+v", rep)
			}
			// The loader's libraries, vanilla's one, and the client jar.
			if rep.Classpath != tc.libraries+2 {
				t.Fatalf("classpath %+v", rep)
			}
			stdout := h.mustRun(t, "-i", "pack", "play", "--dry-run")
			for _, want := range []string{"would launch pack", tc.version, "inherits: 26.2", "loader libraries: " + plural(tc.libraries, "jar", "jars")} {
				if !strings.Contains(stdout, want) {
					t.Fatalf("play --dry-run: %q is missing from\n%s", want, stdout)
				}
			}

			h.mustRun(t, "-i", "pack", "play", "--no-sync")

			argv := waitForFile(t, filepath.Join(gameDir, "args.txt"))
			jar := filepath.Join(store, "libraries", filepath.FromSlash(tc.library))
			for _, want := range []string{tc.mainClass + "\n", jar, filepath.Join(store, "versions", "26.2", "26.2.jar"), "--username\nNotch\n"} {
				if !strings.Contains(argv, want) {
					t.Fatalf("the game's argv is missing %q:\n%s", want, argv)
				}
			}
			if _, err := os.Stat(jar); err != nil {
				t.Fatalf("the loader's library is missing from the store: %v", err)
			}
		})
	}
}

func TestPlayRunsALoaderInstallerOnceWithTheClientJarInPlace(t *testing.T) {
	h := newHarness(t)
	playHarness(t, h, "--loader", "neoforge")

	h.mustRun(t, "-i", "pack", "play", "--dry-run")
	h.mustRun(t, "-i", "pack", "play", "--dry-run")

	if len(h.installs) != 1 {
		t.Fatalf("the installer ran %d times: %v", len(h.installs), h.installs)
	}
	if h.installerFetchedClient {
		t.Fatal("the installer had to download the vanilla client jar, with no progress line of its own")
	}
}

func TestPlayLaunchesOfflineFromTheStore(t *testing.T) {
	for _, loader := range []string{"", "fabric", "quilt", "forge", "neoforge"} {
		t.Run("loader="+loader, func(t *testing.T) {
			h := newHarness(t)
			var args []string
			if loader != "" {
				args = []string{"--loader", loader}
			}
			_, gameDir := playHarness(t, h, args...)
			h.mustRun(t, "accounts", "login", "--use")
			h.mustRun(t, "-i", "pack", "play", "--no-sync")
			argv := filepath.Join(gameDir, "args.txt")
			waitForFile(t, argv)
			if err := os.Remove(argv); err != nil {
				t.Fatal(err)
			}

			h.server.Close()
			h.mustRun(t, "-i", "pack", "play", "--no-sync")
			waitForFile(t, argv)
		})
	}
}
