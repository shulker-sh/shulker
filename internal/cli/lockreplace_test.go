package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestLockReplacesALockItCantRead(t *testing.T) {
	for _, tc := range []struct{ name, lock, code string }{
		{"corrupt", `{"minecraft": `, "lock-invalid"},
		{"foreign", `{"$schema":"https://example.com/lock.json"}`, "lock-invalid"},
		{"no marker", `{"minecraft":"26.2"}`, "lock-invalid"},
		{"newer", `{"$schema":"https://shulker.sh/schema/v9/lock.json"}`, "schema-newer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustRun(t, "create", "--loader", "fabric")
			path := filepath.Join(h.dir, "shulker.lock")
			if err := os.WriteFile(path, []byte(tc.lock), 0o644); err != nil {
				t.Fatal(err)
			}

			code, stdout, _ := h.run(t, "build", "--json")
			var env out.Envelope
			if err := json.Unmarshal([]byte(stdout), &env); err != nil {
				t.Fatal(err)
			}
			if code == 0 || env.Error == nil || env.Error.Code != tc.code {
				t.Fatalf("build over the lock: code=%d %s", code, stdout)
			}
			if tc.code == "lock-invalid" && env.Error.Help != "run `shulker lock`" {
				t.Fatalf("help = %q", env.Error.Help)
			}

			env = out.Envelope{}
			if err := json.Unmarshal([]byte(h.mustRun(t, "lock", "--json")), &env); err != nil {
				t.Fatal(err)
			}
			if len(env.Warnings) != 1 || !strings.HasSuffix(env.Warnings[0], "; replaced it and kept the old one as shulker.lock.replaced") {
				t.Fatalf("warnings: %q", env.Warnings)
			}
			if data, _ := os.ReadFile(path + ".replaced"); string(data) != tc.lock {
				t.Fatalf("replaced = %q", data)
			}
			if l := h.readLock(t); l.Minecraft != "26.2" {
				t.Fatalf("fresh lock: %+v", l)
			}
			if data, _ := os.ReadFile(filepath.Join(h.dir, ".gitignore")); strings.Contains(string(data), "replaced") {
				t.Fatalf(".gitignore: %s", data)
			}
			h.mustRun(t, "lock")
			if data, _ := os.ReadFile(path + ".replaced"); string(data) != tc.lock {
				t.Fatalf("a readable lock was replaced again: %q", data)
			}
		})
	}
}
