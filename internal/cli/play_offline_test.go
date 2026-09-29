package cli

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnUnreachableModpackReadsAsOffline(t *testing.T) {
	for _, kind := range []string{"git", "url"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			shulkerInstances(t, h)
			h.mustRun(t, "config", "set", "store", filepath.Join(t.TempDir(), "store"))
			h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
			h.mustRun(t, "add", "sodium")
			var srv *httptest.Server
			var source string
			if kind == "git" {
				if _, err := exec.LookPath("git"); err != nil {
					t.Skip("git not installed")
				}
				gitRun(t, h.dir, "init", "-q", "-b", "main")
				gitRun(t, h.dir, "add", ".")
				gitRun(t, h.dir, "commit", "-q", "-m", "one")
				served := t.TempDir()
				remote := filepath.Join(served, "remote.git")
				gitRun(t, h.dir, "clone", "-q", "--bare", h.dir, remote)
				gitRun(t, remote, "update-server-info")
				srv = httptest.NewTLSServer(http.FileServer(http.Dir(served)))
				source = srv.URL + "/remote.git"
			} else {
				srv = httptest.NewTLSServer(http.FileServer(http.Dir(h.dir)))
				source = srv.URL + "/shulker.json"
			}
			h.dir = ""
			h.mustRun(t, "link", "shulker", source, "--as", "pack")
			h.mustRun(t, "accounts", "login", "--use")
			srv.Close()

			_, stderr := h.mustRunStderr(t, "sync", "-i", "pack")
			if want := "offline, keeping modpack pack at "; !strings.Contains(stderr, want) {
				t.Fatalf("sync: %q is missing from\n%s", want, stderr)
			}
			_, stderr = h.mustRunStderr(t, "play", "-i", "pack")
			if want := "offline, keeping modpack pack at "; !strings.Contains(stderr, want) || strings.Contains(stderr, "couldn't update") {
				t.Fatalf("play keeps the pin without giving up the relock:\n%s", stderr)
			}
			for _, framing := range []string{"fatal:", "unable to access", `Get "`} {
				if strings.Contains(stderr, framing) {
					t.Fatalf("the tool's own framing %q leaked into the warning:\n%s", framing, stderr)
				}
			}
		})
	}
}
