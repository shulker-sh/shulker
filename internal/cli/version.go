package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
)

const devVersion = "dev"

type versionInfo struct {
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Modified bool   `json:"modified,omitempty"`
	Built    string `json:"built,omitempty"`
	Go       string `json:"go"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Binary   string `json:"binary,omitempty"`
	Config   string `json:"config,omitempty"`
	Cache    string `json:"cache,omitempty"`
}

func (v versionInfo) platform() string { return v.Go + " " + v.OS + "/" + v.Arch }

func (v versionInfo) builtAt() string {
	at, err := time.Parse(time.RFC3339, v.Built)
	if err != nil {
		return v.Built
	}
	return at.UTC().Format("2006-01-02 15:04 UTC")
}

func (v versionInfo) shortCommit() string {
	if len(v.Commit) > 7 {
		return v.Commit[:7]
	}
	return v.Commit
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the shulker version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := versionInfo{Version: version, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
			commit, modified, built := buildVCS()
			info.Built = built
			if version == devVersion {
				info.Commit, info.Modified = commit, modified
			}
			if exe, err := os.Executable(); err == nil {
				if resolved, err := filepath.EvalSymlinks(exe); err == nil {
					exe = resolved
				}
				info.Binary = exe
			}
			if p, err := config.Path(); err == nil {
				info.Config = p
			}
			if c, err := cache.Open(); err == nil {
				info.Cache = c.Dir
			}
			return a.printer.Emit(info, func(l *out.Lines) { printVersion(l, info) })
		},
	}
}

func printVersion(l *out.Lines, info versionInfo) {
	t := l.T
	head := t.Command("shulker " + info.Version)
	if info.Commit != "" {
		head += "  " + t.Grey(info.shortCommit())
		if info.Modified {
			head += t.Yellow("-dirty")
		}
	}
	l.Blank()
	l.Text(head)
	l.Blank()
	row := func(label, value string) {
		if value != "" {
			l.Text(t.Grey(fmt.Sprintf("%-9s", label)) + " " + value)
		}
	}
	row("Built", info.builtAt())
	row("Go", info.platform())
	row("Binary", pathLink(t, info.Binary))
	row("Config", pathLink(t, info.Config))
	row("Cache", pathLink(t, info.Cache))
	l.Blank()
}

func pathLink(t out.Theme, path string) string {
	if path == "" {
		return ""
	}
	return t.Link(homeTilde(path), path)
}

func homeTilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home+string(filepath.Separator)) {
		return path
	}
	return "~" + path[len(home):]
}

// buildVCS reads what the Go toolchain stamps into a binary built inside a worktree. A build from
// an unpacked source tree, a module cache, or -buildvcs=false carries none of it.
func buildVCS() (commit string, modified bool, built string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false, ""
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		case "vcs.time":
			built = s.Value
		}
	}
	return commit, modified, built
}
