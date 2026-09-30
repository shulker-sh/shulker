package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccountsDirReadsTheWindowsLauncherUnderWSL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	appData := t.TempDir()
	windows := filepath.Join(appData, ".minecraft")
	if err := os.Mkdir(windows, 0o755); err != nil {
		t.Fatal(err)
	}
	was := wslAppData
	t.Cleanup(func() { wslAppData = was })
	wslAppData = func() string { return appData }

	if got := mojangEntry.AccountsDir(nil); got != windows {
		t.Fatalf("no launcher on the Linux side, so the Windows one's accounts: got %q", got)
	}
	if got := prismEntry.AccountsDir(nil); got == "" || got == filepath.Join(appData, "PrismLauncher") {
		t.Fatalf("a launcher with no Windows folder keeps its own default: got %q", got)
	}
	linux, err := DefaultMojangDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(linux, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := mojangEntry.AccountsDir(nil); got != linux {
		t.Fatalf("a launcher on the Linux side wins: got %q", got)
	}
}
