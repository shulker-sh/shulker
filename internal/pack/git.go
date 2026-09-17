package pack

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
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
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
	if s.offline() {
		return "", fmt.Errorf("%s: %s: %w", what.label, source, fetch.ErrOffline)
	}
	dir := s.Cache.PackMirror(source)
	if _, err := os.Stat(dir); err == nil {
		s.log("fetching %s", source)
		if _, err := s.git(ctx, "--git-dir="+dir, "fetch", "--quiet", "origin"); err != nil {
			return "", gitFailure(err, what.code, "%s: fetching %s failed: %v", what.label, source, err)
		}
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	s.log("cloning %s", source)
	if _, err := s.git(ctx, "clone", "--quiet", "--mirror", source, dir); err != nil {
		os.RemoveAll(dir)
		return "", gitFailure(err, what.code, "%s: cloning %s failed: %v", what.label, source, err)
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
	"network is unreachable",
	"no route to host",
	"connection reset",
	"ssl_connect",
	"ssl_error_syscall",
	"gnutls_handshake",
	"tls handshake",
}

func gitNetworkError(msg string) bool {
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
		return "", gitFailure(err, what.code, "%s: commit %s is not available from the repository: %v", what.label, commit, err)
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
		target := filepath.Join(dst, rel)
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader:
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777|0o600)
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

func gitFailure(err error, code, format string, args ...any) error {
	if out.CodeOf(err) == "git-missing" {
		return err
	}
	if gitNetworkError(err.Error()) {
		return fetch.Unreachable(out.Errorf(code, format, args...))
	}
	return out.Errorf(code, format, args...)
}
