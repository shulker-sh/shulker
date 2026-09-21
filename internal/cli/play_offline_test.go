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
	for _, tc := range []struct{ kind, reason string }{
		{"git", "git: Failed to connect"},
		{"url", "http: dial tcp"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			h := newHarness(t)
			shulkerInstances(t, h)
			h.mustRun(t, "config", "set", "store", filepath.Join(t.TempDir(), "store"))
			h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
			h.mustRun(t, "add", "sodium")
			var srv *httptest.Server
			var source string
			if tc.kind == "git" {
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
				srv = httptest.NewServer(http.FileServer(http.Dir(served)))
				source = srv.URL + "/remote.git"
			} else {
				srv = httptest.NewServer(http.FileServer(http.Dir(h.dir)))
				source = srv.URL + "/shulker.json"
			}
			h.dir = ""
			h.mustRun(t, "link", "shulker", source, "--as", "pack")
			h.mustRun(t, "accounts", "login", "--use")
			srv.Close()

			code, _, stderr := h.run(t, "-i", "pack", "sync")
			for _, want := range []string{"modpack pack: couldn't reach " + source + " (modpack-fetch)", tc.reason, "help: check the address"} {
				if code == 0 || !strings.Contains(stderr, want) {
					t.Fatalf("sync: exit %d, %q is missing from\n%s", code, want, stderr)
				}
			}
			_, stderr = h.mustRunStderr(t, "-i", "pack", "play")
			if want := "couldn't update, building what the lock already has: modpack pack: couldn't reach " + source + "\n"; !strings.Contains(stderr, want) {
				t.Fatalf("play: %q is missing from\n%s", want, stderr)
			}
			for _, framing := range []string{"fatal:", "unable to access", `Get "`} {
				if strings.Contains(stderr, framing) {
					t.Fatalf("the tool's own framing %q leaked into the warning:\n%s", framing, stderr)
				}
			}
		})
	}
}
