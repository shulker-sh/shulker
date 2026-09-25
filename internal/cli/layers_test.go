package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func twoSided(t *testing.T, h *harness) {
	t.Helper()
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true}
	})
}

func dataJSON(t *testing.T, stdout string) string {
	t.Helper()
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	data, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAnUnreadableDataVersionLeavesTheLockToTheNextRelock(t *testing.T) {
	h := newHarness(t)
	h.noRanges = true
	code, _, stderr := h.run(t, "create", "--loader", "fabric")
	if code != 0 || !strings.Contains(stderr, "couldn't read the Minecraft 26.2 data version") {
		t.Fatalf("an unreadable data version warns rather than failing the lock: %d %s", code, stderr)
	}
	if l := h.readLock(t); l.DataVersion != 0 {
		t.Fatalf("dataVersion = %d, want unset", l.DataVersion)
	}
	writeFile(t, filepath.Join(h.dir, "overrides", "v.txt.tmpl"), "${minecraft.dataVersion}")
	code, _, stderr = h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "help: run shulker lock") {
		t.Fatalf("an unset data version says how to fill it: %d %s", code, stderr)
	}

	h.noRanges = false
	h.mustRun(t, "lock")
	if l := h.readLock(t); l.DataVersion != 4903 {
		t.Fatalf("the next relock fills the data version: %d", l.DataVersion)
	}
}
