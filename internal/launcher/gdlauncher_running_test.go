package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGDLauncherRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("GDLauncher leaves no lock file on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	appData, err := gdlauncherAppData()
	if err != nil {
		t.Fatal(err)
	}
	if running, detectable := GDLauncherRunning(); running || !detectable {
		t.Fatalf("no lock: running %v, detectable %v", running, detectable)
	}
	if err := os.MkdirAll(appData, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(appData, "SingletonLock")
	if err := os.Symlink(fmt.Sprintf("some-mac.local-%d", os.Getpid()), lock); err != nil {
		t.Fatal(err)
	}
	if running, _ := GDLauncherRunning(); !running {
		t.Fatal("a lock naming a live process should read as running")
	}

	exited := exec.Command("true")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fmt.Sprintf("some-mac.local-%d", exited.Process.Pid), lock); err != nil {
		t.Fatal(err)
	}
	if running, detectable := GDLauncherRunning(); running || !detectable {
		t.Fatalf("a stale lock: running %v, detectable %v", running, detectable)
	}
}
