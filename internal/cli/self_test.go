package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/selfupdate"
)

type selfUpdateHarness struct {
	app            *app
	exe            string
	stdout, stderr bytes.Buffer
}

func newSelfUpdateHarness(t *testing.T, current, tag string, corrupt bool) *selfUpdateHarness {
	t.Helper()
	old := version
	version = current
	t.Cleanup(func() { version = old })
	t.Setenv("PATH", t.TempDir())

	archive := releaseArchive(t, "new binary")
	sum := sha256.Sum256(archive)
	checksum := hex.EncodeToString(sum[:])
	if corrupt {
		checksum = strings.Repeat("0", len(checksum))
	}
	asset := selfupdate.AssetName(tag, runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	})
	mux.HandleFunc("/download/"+tag+"/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", checksum, asset)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	h := &selfUpdateHarness{exe: filepath.Join(t.TempDir(), "shulker")}
	if err := os.WriteFile(h.exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.app = newApp(&h.stdout, &h.stderr)
	h.app.configPath = filepath.Join(t.TempDir(), "config.json")
	h.app.build = func() selfupdate.Build { return selfupdate.Describe(current, "", nil) }
	h.app.releases = selfupdate.New(fetch.New("test"))
	h.app.releases.LatestURL = server.URL + "/latest"
	h.app.releases.DownloadURL = server.URL + "/download"
	h.app.exe = func() (string, error) { return h.exe, nil }
	return h
}

func (s *selfUpdateHarness) run(args ...string) int {
	return s.app.run(context.Background(), append([]string{"self", "update"}, args...))
}

func (s *selfUpdateHarness) binary(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(s.exe)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (s *selfUpdateHarness) errorCode(t *testing.T) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(s.stdout.Bytes(), &env); err != nil {
		t.Fatalf("%v: %s", err, &s.stdout)
	}
	return env.Error.Code
}

func releaseArchive(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("shulker.exe")
		w.Write([]byte(body))
		zw.Close()
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "shulker", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write([]byte(body))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestSelfUpdateReplacesBinary(t *testing.T) {
	h := newSelfUpdateHarness(t, "0.0.1", "v0.0.2", false)
	if code := h.run(); code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, &h.stdout, &h.stderr)
	}
	if got := h.binary(t); got != "new binary" {
		t.Fatalf("binary holds %q", got)
	}
	if want := "  ✔ updated shulker 0.0.1 ⟶ 0.0.2 » " + h.exe + "\n"; h.stdout.String() != want {
		t.Fatalf("stdout %q, want %q", &h.stdout, want)
	}
	for _, line := range []string{"checksum verified", "gh not found, skipping build provenance check"} {
		if !strings.Contains(h.stderr.String(), line) {
			t.Fatalf("stderr missing %q:\n%s", line, &h.stderr)
		}
	}
}

func TestSelfUpdateUpToDate(t *testing.T) {
	h := newSelfUpdateHarness(t, "0.0.2", "v0.0.2", false)
	if code := h.run(); code != 0 {
		t.Fatalf("exit %d: %s", code, &h.stderr)
	}
	if h.stdout.String() != "  ✔ shulker is up to date (0.0.2)\n" || h.binary(t) != "old binary" {
		t.Fatalf("stdout %q, binary %q", &h.stdout, h.binary(t))
	}
}

func TestSelfUpdateCheckOnlyReports(t *testing.T) {
	h := newSelfUpdateHarness(t, "0.0.1", "v0.0.2", false)
	if code := h.run("--check", "--json"); code != 0 {
		t.Fatalf("exit %d: %s", code, &h.stdout)
	}
	var env struct {
		Data selfUpdateResult `json:"data"`
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Available == nil || !*env.Data.Available || env.Data.Current != "0.0.1" || env.Data.Latest != "0.0.2" || env.Data.Install != "release" || h.binary(t) != "old binary" {
		t.Fatalf("data %+v, binary %q", env.Data, h.binary(t))
	}
}

func TestSelfUpdateRefusesABinaryItDidNotInstall(t *testing.T) {
	for _, tc := range []struct {
		build                  selfupdate.Build
		message, lead, command string
	}{
		{selfupdate.Build{Version: "0.0.1", Route: selfupdate.GoInstall}, "this shulker was installed with go install", "Update it with", "go install shulker.sh/shulker@latest"},
		{selfupdate.Build{Version: "dev", Commit: "d1556f95d232", Route: selfupdate.Source}, "this shulker was built from source at d1556f9", "Rebuild it with", "go build ."},
		{selfupdate.Build{Version: "dev"}, "this shulker was built from source", "Rebuild it with", "go build ."},
		{selfupdate.Build{Version: "0.0.1", Route: selfupdate.Homebrew}, "this shulker was installed by Homebrew", "Update it with", "brew upgrade shulker"},
		{selfupdate.Build{Version: "0.0.1", Route: selfupdate.Scoop}, "this shulker was installed by Scoop", "Update it with", "scoop update shulker"},
	} {
		h := newSelfUpdateHarness(t, "0.0.1", "v0.0.2", false)
		h.app.build = func() selfupdate.Build { return tc.build }
		if code := h.run(); code == 0 {
			t.Fatalf("%s: expected a refusal", tc.build.Route)
		}
		if got := h.stderr.String(); !strings.Contains(got, tc.message+" (self-update-unmanaged)") || !strings.Contains(got, tc.lead+":\n    $ "+tc.command+"\n") {
			t.Fatalf("%s: stderr %q", tc.build.Route, got)
		}
		if h.binary(t) != "old binary" {
			t.Fatalf("%s: the binary was touched", tc.build.Route)
		}
		h.stdout.Reset()
		if code := h.run("--json"); code == 0 || h.errorCode(t) != "self-update-unmanaged" {
			t.Fatalf("%s: %s", tc.build.Route, &h.stdout)
		}
	}
}

func TestSelfUpdateCheckWorksOnEveryRoute(t *testing.T) {
	h := newSelfUpdateHarness(t, "0.0.1", "v0.0.2", false)
	h.app.build = func() selfupdate.Build { return selfupdate.Build{Version: "0.0.1", Route: selfupdate.GoInstall} }
	if code := h.run("--check"); code != 0 {
		t.Fatalf("exit %d: %s", code, &h.stdout)
	}
	if got := h.stdout.String(); !strings.Contains(got, "shulker 0.0.1 ⟶ 0.0.2 (update available)") || !strings.Contains(got, "Update it with:\n    $ go install shulker.sh/shulker@latest\n") {
		t.Fatalf("go install --check: %q", got)
	}

	h = newSelfUpdateHarness(t, "0.0.1", "v0.0.2", false)
	h.app.build = func() selfupdate.Build {
		return selfupdate.Build{Version: "dev", Commit: "d1556f95d232", Route: selfupdate.Source}
	}
	if code := h.run("--check"); code != 0 {
		t.Fatalf("exit %d: %s", code, &h.stdout)
	}
	if got := h.stdout.String(); got != "  i the latest release is shulker 0.0.2\n\n  Rebuild it with:\n    $ go build .\n" {
		t.Fatalf("source --check: %q", got)
	}
	h.stdout.Reset()
	if code := h.run("--check", "--json"); code != 0 {
		t.Fatalf("exit %d: %s", code, &h.stdout)
	}
	var env struct {
		Data selfUpdateResult `json:"data"`
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Available != nil || env.Data.Current != "dev" || env.Data.Install != "source" || !strings.Contains(h.stdout.String(), `"available": null`) {
		t.Fatalf("a source build's availability is unknown: %s", &h.stdout)
	}
}

func TestSelfUpdateChecksumMismatchKeepsBinary(t *testing.T) {
	h := newSelfUpdateHarness(t, "0.0.1", "v0.0.2", true)
	if code := h.run("--json"); code == 0 {
		t.Fatal("expected a failure")
	}
	if h.errorCode(t) != "self-update-checksum" || h.binary(t) != "old binary" {
		t.Fatalf("stdout %s, binary %q", &h.stdout, h.binary(t))
	}
}

func TestSelfUpdateRequireAttestationWithoutGh(t *testing.T) {
	h := newSelfUpdateHarness(t, "0.0.1", "v0.0.2", false)
	if code := h.run("--require-attestation", "--json"); code == 0 {
		t.Fatal("expected a failure")
	}
	if h.errorCode(t) != "self-update-provenance" || h.binary(t) != "old binary" {
		t.Fatalf("stdout %s, binary %q", &h.stdout, h.binary(t))
	}
}
