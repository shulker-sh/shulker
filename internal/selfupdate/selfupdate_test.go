package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestAssetName(t *testing.T) {
	if got := AssetName("v0.0.1", "darwin", "arm64"); got != "shulker_0.0.1_darwin_arm64.tar.gz" {
		t.Fatalf("darwin: %s", got)
	}
	if got := AssetName("v0.0.1", "windows", "amd64"); got != "shulker_0.0.1_windows_amd64.zip" {
		t.Fatalf("windows: %s", got)
	}
}

func TestNeedsUpdate(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.0.1", "v0.0.2", true},
		{"0.0.2", "v0.0.2", false},
		{"0.0.3", "v0.0.2", false},
		{"0.0.9", "v0.1.0", true},
		{"0.0.0-SNAPSHOT-7efc987", "v0.0.1", true},
		{"0.0.2-rc1", "v0.0.2", false},
	}
	for _, c := range cases {
		if got := NeedsUpdate(c.current, c.latest); got != c.want {
			t.Errorf("NeedsUpdate(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	sums := ParseChecksums("abc  shulker_0.0.1_linux_amd64.tar.gz\ndef  checksums.txt\n\n")
	if sums["shulker_0.0.1_linux_amd64.tar.gz"] != "abc" || sums["checksums.txt"] != "def" || len(sums) != 2 {
		t.Fatalf("got %v", sums)
	}
}

func TestInstallFromTarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "shulker_0.0.2_linux_amd64.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"CHANGELOG.md": "notes", "shulker": "new binary"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	os.WriteFile(archive, buf.Bytes(), 0o644)
	exe := filepath.Join(dir, "shulker")
	os.WriteFile(exe, []byte("old binary"), 0o755)

	if err := Install(archive, exe); err != nil {
		t.Fatal(err)
	}
	assertExecutable(t, exe, "new binary")
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatalf("staged copy left behind: %v", err)
	}
}

func TestExtractFromZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "shulker_0.0.2_windows_amd64.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("shulker.exe")
	w.Write([]byte("new exe"))
	zw.Close()
	os.WriteFile(archive, buf.Bytes(), 0o644)

	dest := filepath.Join(dir, "out")
	if err := extractBinary(archive, dest); err != nil {
		t.Fatal(err)
	}
	assertExecutable(t, dest, "new exe")
}

func TestExtractMissingBinary(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "shulker_0.0.2_linux_amd64.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tar.NewWriter(gz).Close()
	gz.Close()
	os.WriteFile(archive, buf.Bytes(), 0o644)
	exe := filepath.Join(dir, "shulker")
	os.WriteFile(exe, []byte("old binary"), 0o755)

	if err := Install(archive, exe); err == nil {
		t.Fatal("expected an error for an archive without shulker")
	}
	assertExecutable(t, exe, "old binary")
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatalf("staged copy left behind: %v", err)
	}
}

func assertExecutable(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s holds %q, want %q", path, data, want)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s is not executable: %v", path, info.Mode())
	}
}
