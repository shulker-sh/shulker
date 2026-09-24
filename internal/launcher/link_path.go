package launcher

import (
	"os"
	"os/exec"
	"path/filepath"
)

// ShulkerPath is the path reconcile records in settings.shulker for the generated scripts to call:
// the one shulker is installed under on PATH when that is this binary, else this binary's own
// path. A bare "shulker" would not do, because launchers opened from the Dock never read the shell
// rc files installers add PATH through.
func ShulkerPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	onPath, err := exec.LookPath("shulker")
	if err != nil {
		return exe, nil
	}
	if onPath, err = filepath.Abs(onPath); err != nil {
		return exe, nil
	}
	a, err := os.Stat(onPath)
	if err != nil {
		return exe, nil
	}
	b, err := os.Stat(exe)
	if err != nil || !os.SameFile(a, b) {
		return exe, nil
	}
	return onPath, nil
}
