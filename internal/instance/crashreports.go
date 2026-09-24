package instance

import (
	"os"
	"path/filepath"
	"time"
)

// FailureFiles returns the game's log and the newest crash report written since started, each
// empty when there isn't one.
func FailureFiles(dir string, started time.Time) (log, crashReport string) {
	if path := filepath.Join(dir, "logs", "latest.log"); isOnDisk(path) {
		log = path
	}
	return log, CrashReportSince(dir, started)
}

// CrashReportSince is the newest crash report the game wrote after started, empty when there is
// none. It is the only evidence of how a run ended that survives the process that ran it.
func CrashReportSince(dir string, started time.Time) string {
	entries, _ := os.ReadDir(filepath.Join(dir, "crash-reports"))
	var newest time.Time
	var report string
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() || info.ModTime().Before(started) || !info.ModTime().After(newest) {
			continue
		}
		newest = info.ModTime()
		report = filepath.Join(dir, "crash-reports", e.Name())
	}
	return report
}

func isOnDisk(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
