package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestInstallTriesEveryDownloadUnlessFailFast(t *testing.T) {
	h := newHarness(t)
	sodium, api := h.jars["sodium"], h.jars["fabric-api"]
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "sodium", "fabric-api")
	os.RemoveAll(h.cache)
	h.cdnDown = map[string]bool{"/cdn/" + sodium.filename: true, "/cdn/" + api.filename: true}

	code, stdout, _ := h.run(t, "--json", "install")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "download-failed" || e.Message != "couldn't download 2 files" || len(e.Items) != 2 || !strings.Contains(e.Help, "shulker update") {
		t.Fatalf("every failed download in one error: exit %d: %s", code, stdout)
	}
	code, _, stderr := h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "/cdn/"+sodium.filename) || !strings.Contains(stderr, "/cdn/"+api.filename) {
		t.Fatalf("each file's rows: exit %d: %s", code, stderr)
	}

	code, stdout, _ = h.run(t, "--json", "install", "--fail-fast")
	if e := failureCode(t, stdout); code == 0 || e.Code != "download-failed" || len(e.Items) != 0 || !strings.HasPrefix(e.Message, "couldn't download ") || strings.Contains(e.Message, "2 files") {
		t.Fatalf("--fail-fast stops at the first: exit %d: %s", code, stdout)
	}
}

func TestCheckReportsFailedDownloadsAndManualDownloadsApart(t *testing.T) {
	h := newHarness(t)
	sodium := h.jars["sodium"]
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	downloads := filepath.Join(h.dir, "downloads")
	os.MkdirAll(downloads, 0o755)
	os.WriteFile(filepath.Join(downloads, "nodist-1.0.0.jar"), h.jars["nodist"].data, 0o644)
	h.mustRun(t, "add", "nodist", "sodium")
	os.RemoveAll(downloads)
	os.RemoveAll(h.cache)
	h.cdnDown = map[string]bool{"/cdn/" + sodium.filename: true}

	code, env := runCheck(t, h)
	if code == 0 || env.problem("download-failed") == nil || env.problem("missing-files") == nil {
		t.Fatalf("both problems: exit %d %+v", code, env)
	}

	code, stdout, _ := h.run(t, "--json", "install")
	var got struct {
		Error *struct{ Code string } `json:"error"`
		Data  struct {
			Errors []struct{ Code string } `json:"errors"`
		} `json:"data"`
	}
	json.Unmarshal([]byte(stdout), &got)
	if code == 0 || got.Error == nil || got.Error.Code != "missing-files" || len(got.Data.Errors) != 1 || got.Data.Errors[0].Code != "download-failed" {
		t.Fatalf("install fails with both: exit %d: %s", code, stdout)
	}
}

func TestSyncServeAndExportTryEveryDownloadUnlessFailFast(t *testing.T) {
	for _, tc := range []struct {
		side string
		args []string
	}{
		{"client", []string{"sync"}},
		{"server", []string{"serve", "--accept-eula"}},
		{"client", []string{"export", "mrpack", "--version", "1.0.0"}},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			h := newHarness(t)
			sodium, api := h.jars["sodium"], h.jars["fabric-api"]
			h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--side", tc.side)
			h.editManifest(t, func(m map[string]any) {
				m[tc.side].(map[string]any)["build"] = "."
			})
			h.mustRun(t, "add", "sodium", "fabric-api", "--side", tc.side)
			os.RemoveAll(h.cache)
			h.cdnDown = map[string]bool{"/cdn/" + sodium.filename: true, "/cdn/" + api.filename: true}

			code, stdout, _ := h.run(t, append([]string{"--json"}, tc.args...)...)
			if e := failureCode(t, stdout); code == 0 || e.Code != "download-failed" || len(e.Items) != 2 {
				t.Fatalf("every failed download in one error: exit %d: %s", code, stdout)
			}
			code, stdout, _ = h.run(t, append([]string{"--json"}, append(tc.args, "--fail-fast")...)...)
			if e := failureCode(t, stdout); code == 0 || e.Code != "download-failed" || len(e.Items) != 0 {
				t.Fatalf("--fail-fast stops at the first: exit %d: %s", code, stdout)
			}
		})
	}
}
