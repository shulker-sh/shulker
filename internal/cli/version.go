package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
)

const devVersion = "dev"

type versionInfo struct {
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Modified bool   `json:"modified,omitempty"`
	Go       string `json:"go"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

// build is what identifies this binary, in parentheses after the version. A released version names
// itself; a local one is only pinned down by the commit it came from and whether that tree was clean.
func (v versionInfo) build() string {
	platform := v.Go + " " + v.OS + "/" + v.Arch
	if v.Commit == "" {
		return platform
	}
	commit := v.Commit
	if len(commit) > 7 {
		commit = commit[:7]
	}
	if v.Modified {
		commit += "-dirty"
	}
	return commit + ", " + platform
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the shulker version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := versionInfo{Version: version, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
			if version == devVersion {
				info.Commit, info.Modified = buildVCS()
			}
			return a.printer.Emit(info, func(l *out.Lines) {
				l.Raw(fmt.Sprintf("shulker %s (%s)", info.Version, info.build()))
			})
		},
	}
}

// buildVCS reads what the Go toolchain stamps into a binary built inside a worktree. A build from
// an unpacked source tree, a module cache, or -buildvcs=false carries none of it.
func buildVCS() (commit string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return commit, modified
}
