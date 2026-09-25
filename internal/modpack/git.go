package modpack

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
)

func (s *Store) git(ctx context.Context, args ...string) ([]byte, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, out.Errorf("git-missing", "git is required for git sources but was not found in PATH")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	// A server that accepts the connection and then sends nothing would otherwise hang git forever.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_HTTP_LOW_SPEED_LIMIT=1", "GIT_HTTP_LOW_SPEED_TIME=60")
	killGroup(cmd)
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return stdout.Bytes(), nil
}

func (s *Store) ensureMirror(ctx context.Context, what origin, source string) (string, error) {
	if s.isOffline() {
		return "", fmt.Errorf("%s: %s: %w", what.label, source, fetch.ErrOffline)
	}
	dir := s.Cache.PackMirror(source)
	if _, err := os.Stat(dir); err == nil {
		s.log("fetching %s", source)
		if _, err := s.git(ctx, "--git-dir="+dir, "fetch", "--quiet", "origin"); err != nil {
			return "", mirrorFailure(err, what, source, "fetching")
		}
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	s.log("cloning %s", source)
	if _, err := s.git(ctx, "clone", "--quiet", "--mirror", source, dir); err != nil {
		os.RemoveAll(dir)
		return "", mirrorFailure(err, what, source, "cloning")
	}
	return dir, nil
}

var gitNetworkErrors = []string{
	"could not resolve host",
	"could not resolve hostname",
	"failed to connect",
	"couldn't connect to server",
	"connection refused",
	"connection timed out",
	"operation timed out",
	"operation too slow",
	"network is unreachable",
	"no route to host",
	"connection reset",
	"ssl_connect",
	"ssl_error_syscall",
	"gnutls_handshake",
	"tls handshake",
}

func isGitNetworkError(msg string) bool {
	msg = strings.ToLower(msg)
	for _, s := range gitNetworkErrors {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func (s *Store) revParse(ctx context.Context, mirror, ref string) (string, error) {
	data, err := s.git(ctx, "--git-dir="+mirror, "rev-parse", "--verify", "--quiet", refOrHead(ref)+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *Store) export(ctx context.Context, what origin, mirror, commit string) (string, error) {
	dir := s.Cache.PackSource(commit)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	archive, err := s.git(ctx, "--git-dir="+mirror, "archive", "--format=tar", commit)
	if err != nil {
		return "", gitFailure(err, what.code, "%s: commit %s is not available from the repository", what.label, commit)
	}
	tmp, err := s.Cache.TempDir("src")
	if err != nil {
		return "", err
	}
	if err := untar(bytes.NewReader(archive), tmp); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		if _, statErr := os.Stat(dir); statErr == nil {
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}

func untar(r io.Reader, dst string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel := filepath.Clean(filepath.FromSlash(hdr.Name))
		if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		entryPath := filepath.Join(dst, rel)
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader:
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(entryPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(entryPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("pack archive: unsupported entry %s", hdr.Name)
		}
	}
}

type origin struct {
	label string
	code  string
}

func packOrigin(name string) origin { return origin{label: "modpack " + name, code: "modpack-fetch"} }

// mirrorFailure is a fetch or clone that failed. When the network is why, the headline names the
// source once and git's own reason goes in a row, without the "fatal:" and repeated URL around it.
func mirrorFailure(err error, what origin, source, verb string) error {
	if out.CodeOf(err) != "git-missing" && isGitNetworkError(err.Error()) {
		e := out.Errorf(what.code, "%s: couldn't reach %s", what.label, source)
		e.Rows = []out.Detail{{Label: "git", Text: gitReason(err.Error())}}
		e.Help = unreachableHelp
		return fetch.Unreachable(e)
	}
	return gitFailure(err, what.code, "%s: %s %s failed", what.label, verb, source)
}

const unreachableHelp = "check the address and that the server is running, then try again"

var gitFraming = regexp.MustCompile(`^(?:fatal: )?(?:unable to access '[^']*': )?`)

func gitReason(msg string) string { return strings.TrimSpace(gitFraming.ReplaceAllString(msg, "")) }

func gitFailure(err error, code, format string, args ...any) error {
	if out.CodeOf(err) == "git-missing" {
		return err
	}
	e := out.Errorf(code, format, args...)
	e.Rows = []out.Detail{{Label: "git", Text: gitReason(err.Error())}}
	if isGitNetworkError(err.Error()) {
		return fetch.Unreachable(e)
	}
	return e
}
