package server

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/out"
)

// RunInstaller runs a loader's installer jar with java. Installers write <jar>.log next to the jar,
// so it runs from a copy in a temporary directory, keeping the log out of the cache and server dir.
func RunInstaller(ctx context.Context, java, jar string, args []string) error {
	tmp, err := os.MkdirTemp("", "shulker-installer-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	installer := filepath.Join(tmp, "installer.jar")
	if err := copyFile(jar, installer); err != nil {
		return err
	}
	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, java, append([]string{"-jar", installer}, args...)...)
	cmd.Dir = tmp
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		last := lastLines(output.String(), 15)
		e := out.Errorf("installer-failed", "the loader installer failed (%v); its last output:\n%s", err, last)
		for _, line := range strings.Split(last, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				e.Rows = append(e.Rows, out.Detail{Text: line})
			}
		}
		return &InstallerFailure{Err: e, Output: output.String()}
	}
	return nil
}

// InstallerFailure carries the installer's whole output so the caller can save it; errors.As
// still finds the installer-failed *out.Error inside.
type InstallerFailure struct {
	Err    *out.Error
	Output string
}

func (f *InstallerFailure) Error() string { return f.Err.Message }

func (f *InstallerFailure) Unwrap() error { return f.Err }

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, in); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
