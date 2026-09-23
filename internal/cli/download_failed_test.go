package cli

import (
	"os"
	"strings"
	"testing"
)

func TestInstallNamesTheFileAProviderFailsToServe(t *testing.T) {
	h := newHarness(t)
	sodium := h.jars["sodium"]
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	os.RemoveAll(h.cache)

	h.cdnCut = map[string]bool{"/cdn/" + sodium.filename: true}
	code, stdout, _ := h.run(t, "--json", "install")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "download-failed" || !strings.Contains(e.Message, "sodium") || !strings.Contains(e.Message, sodium.filename) || !strings.Contains(e.Message, "Modrinth") || !strings.Contains(e.Help, "partial file") {
		t.Fatalf("cut short: exit %d: %s", code, stdout)
	}
	code, _, stderr := h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "/cdn/"+sodium.filename) || !strings.Contains(stderr, "unexpected EOF") {
		t.Fatalf("cut short rows: exit %d: %s", code, stderr)
	}

	h.cdnCut, h.cdnDown = nil, map[string]bool{"/cdn/" + sodium.filename: true}
	code, stdout, _ = h.run(t, "--json", "install")
	if e := failureCode(t, stdout); code == 0 || e.Code != "download-failed" || !strings.Contains(e.Help, "shulker update") {
		t.Fatalf("not found: exit %d: %s", code, stdout)
	}

	h.cdnDown, h.cdnDrop = nil, map[string]bool{"/cdn/" + sodium.filename: true}
	code, stdout, _ = h.run(t, "--json", "install")
	if e := failureCode(t, stdout); code == 0 || e.Code != "download-failed" || !strings.Contains(e.Help, "connection to Modrinth failed") {
		t.Fatalf("dropped: exit %d: %s", code, stdout)
	}
	code, _, stderr = h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "cause: EOF") {
		t.Fatalf("dropped rows: exit %d: %s", code, stderr)
	}

	h.cdnDrop = nil
	h.jars["sodium"] = fakeJar{filename: sodium.filename, data: []byte("not sodium")}
	code, _, stderr = h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "checksum-mismatch") || !strings.Contains(stderr, "sodium ("+sodium.filename+")") {
		t.Fatalf("mismatch: exit %d: %s", code, stderr)
	}
}
