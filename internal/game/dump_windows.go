package game

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"shulker.sh/shulker/internal/instance"
)

// dump runs the runtime's own jcmd, which prints the dump to its stdout rather than the game's, so
// the dump is appended to the run's log to keep every dump of the run in one place.
func dump(run instance.Launch, timeout time.Duration) (string, error) {
	jcmd := filepath.Join(filepath.Dir(run.Java), "jcmd.exe")
	if _, err := os.Stat(jcmd); errors.Is(err, fs.ErrNotExist) {
		return "", &JcmdNotFoundError{Path: jcmd}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, jcmd, strconv.Itoa(run.PID), "Thread.print").Output()
	text, ok := cutDump(string(out))
	if !ok {
		var exit *exec.ExitError
		switch {
		case ctx.Err() != nil:
			return "", ErrDumpTimeout
		case errors.As(err, &exit):
			return "", &JcmdFailedError{Stderr: strings.TrimSpace(string(exit.Stderr))}
		case err != nil:
			return "", err
		}
		return "", ErrDumpTimeout
	}
	f, err := os.OpenFile(run.Log, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(text + "\n"); err != nil {
		return "", err
	}
	return text, nil
}
