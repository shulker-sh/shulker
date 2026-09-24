package instance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFailureFiles(t *testing.T) {
	dir := t.TempDir()
	started := time.Now().Add(-time.Minute)
	if log, crash := FailureFiles(dir, started); log != "" || crash != "" {
		t.Fatalf("empty dir: log %q, crash report %q", log, crash)
	}

	write := func(rel string, at time.Time) string {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
		return path
	}
	logPath := write("logs/latest.log", time.Now())
	write("crash-reports/crash-old-server.txt", started.Add(-time.Hour))
	write("crash-reports/crash-first-server.txt", started.Add(10*time.Second))
	newest := write("crash-reports/crash-last-server.txt", started.Add(20*time.Second))

	if log, crash := FailureFiles(dir, started); log != logPath || crash != newest {
		t.Fatalf("log %q, crash report %q; want %q and %q", log, crash, logPath, newest)
	}
}
