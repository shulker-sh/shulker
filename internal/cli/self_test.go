package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
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
	h.app.releases = selfupdate.New(fetch.New("test"))
	h.app.releases.LatestURL = server.URL + "/latest"
	h.app.releases.DownloadURL = server.URL + "/download"
	h.app.exe = func() (string, error) { return h.exe, nil }
	return h
}

func (h *selfUpdateHarness) run(args ...string) int {
	return h.app.run(append([]string{"self", "update"}, args...))
}

func (h *selfUpdateHarness) binary(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(h.exe)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (h *selfUpdateHarness) errorCode(t *testing.T) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &env); err != nil {
		t.Fatalf("%v: %s", err, &h.stdout)
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
	if want := "updated shulker 0.0.1 -> 0.0.2 at " + h.exe + "\n"; h.stdout.String() != want {
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
	if h.stdout.String() != "shulker is up to date (0.0.2)\n" || h.binary(t) != "old binary" {
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
	want := selfUpdateResult{Current: "0.0.1", Latest: "0.0.2", Available: true}
	if env.Data != want || h.binary(t) != "old binary" {
		t.Fatalf("data %+v, binary %q", env.Data, h.binary(t))
	}
}

func TestSelfUpdateChecksumMismatchKeepsBinary(t *testing.T) {
	h := newSelfUpdateHarness(t, "0.0.1", "v0.0.2", true)
	if code := h.run("--json"); code == 0 {
		t.Fatal("expected a failure")
	}
	if h.errorCode(t) != "update-checksum" || h.binary(t) != "old binary" {
		t.Fatalf("stdout %s, binary %q", &h.stdout, h.binary(t))
	}
}

func TestSelfUpdateRequireAttestationWithoutGh(t *testing.T) {
	h := newSelfUpdateHarness(t, "0.0.1", "v0.0.2", false)
	if code := h.run("--require-attestation", "--json"); code == 0 {
		t.Fatal("expected a failure")
	}
	if h.errorCode(t) != "update-provenance" || h.binary(t) != "old binary" {
		t.Fatalf("stdout %s, binary %q", &h.stdout, h.binary(t))
	}
}
