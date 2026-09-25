//go:build !windows

package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/lock"
)

func TestForgeServer(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--name", "pack", "--loader", "forge", "--side", "server")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true}
	})

	h.mustRun(t, "install")
	var l lock.Lock
	h.readJSON(t, "shulker.lock", &l)
	if l.Loader.Type != "forge" || l.Loader.Version != "65.1.3" || l.Loader.Server == nil || l.Loader.Server.Sha512 != h.forgeInstaller.sha512 {
		t.Fatalf("lock loader: %+v", l.Loader)
	}

	buildDir := filepath.Join(h.dir, "build", "server")
	if len(h.installs) != 1 || strings.Join(h.installs[0], " ") != "--installServer "+buildDir+" --offline" {
		t.Fatalf("installer runs: %v", h.installs)
	}
	if got := build.LoadState(buildDir).InstalledLoader; got == nil || *got != (build.InstalledLoader{Type: "forge", Version: "65.1.3"}) {
		t.Fatalf("state loader: %+v", got)
	}

	h.stdin = strings.NewReader("stop\n")
	h.mustRun(t, "serve")
	args := readFile(t, filepath.Join(buildDir, "args.txt"))
	if !strings.HasSuffix(args, "\n@libraries/net/minecraftforge/forge/26.2-65.1.3/unix_args.txt\n--nogui\n") {
		t.Fatalf("serve args:\n%s", args)
	}
}
