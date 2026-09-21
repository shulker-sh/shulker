package saves

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// linkDir makes a directory junction rather than a symlink, because a junction needs neither
// administrator rights nor Developer Mode.
func linkDir(target, link string) error {
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %s: %s", link, strings.TrimSpace(string(output)))
	}
	return nil
}

// unlinkDir removes the junction itself; RemoveDirectory never follows it into the group.
func unlinkDir(link string) error { return os.Remove(link) }
