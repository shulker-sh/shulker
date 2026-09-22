package fsutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LinkDir makes a directory junction rather than a symlink, because a junction needs neither
// administrator rights nor Developer Mode. A junction's target must be absolute, so a relative
// target is resolved against link's folder, and a moved target needs linking again.
func LinkDir(target, link string) error {
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %s: %s", link, strings.TrimSpace(string(output)))
	}
	return nil
}

// UnlinkDir removes the junction itself; RemoveDirectory never follows it into its target.
func UnlinkDir(link string) error { return os.Remove(link) }
