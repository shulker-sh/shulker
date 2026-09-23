package game

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// dump runs the runtime's own jcmd, which prints the dump to its stdout rather than the game's, so
// the dump is appended to the run's log to keep every dump of the run in one place.
func dump(pid int, java, log string, timeout time.Duration) (string, error) {
	jcmd := filepath.Join(filepath.Dir(java), "jcmd.exe")
	if _, err := os.Stat(jcmd); errors.Is(err, fs.ErrNotExist) {
		return "", &JcmdNotFoundError{Path: jcmd}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, jcmd, strconv.Itoa(pid), "Thread.print").Output()
	text, ok := cutDump(string(out))
	if !ok {
		if err != nil {
			return "", err
		}
		return "", ErrDumpTimeout
	}
	f, err := os.OpenFile(log, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(text + "\n"); err != nil {
		return "", err
	}
	return text, nil
}
