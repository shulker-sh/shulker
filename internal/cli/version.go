package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/selfupdate"
)

// version and date are set by the release build's ldflags; a source or `go install` build leaves
// them empty and is described from its build info instead.
var version, date string

type versionInfo struct {
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Modified bool   `json:"modified,omitempty"`
	Built    string `json:"built,omitempty"`
	Install  string `json:"install,omitempty"`
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
	var verbose bool
	cmd := &cobra.Command{
		Use:         "version",
		Annotations: reads(),
		Short:       "Print the shulker version",
		Args:        noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			b := a.build()
			info := versionInfo{Version: b.Version, Commit: b.Commit, Modified: b.Modified, Built: b.Built, Install: string(b.Route), Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
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
			return a.printer.Emit(info, func(l *out.Lines) { printVersion(l, info, verbose) })
		},
	}
	cmd.Flags().BoolVar(&verbose, "verbose", false, "also print the build and the environment: Go, install route, binary, config and cache")
	return cmd
}

func printVersion(l *out.Lines, info versionInfo, verbose bool) {
	t := l.T
	head := t.Command("shulker " + info.Version)
	if info.Commit != "" {
		head += "  " + t.Grey(info.shortCommit())
		if info.Modified {
			head += t.Yellow("-dirty")
		}
	}
	if !verbose && info.Built != "" {
		head += t.Aside("built " + info.builtAt())
	}
	l.Text(head)
	if !verbose {
		return
	}
	l.Blank()
	row := func(label, value string) {
		if value != "" {
			l.Text(t.Grey(fmt.Sprintf("%-9s", label)) + " " + value)
		}
	}
	row("Built", info.builtAt())
	row("Go", info.platform())
	row("Install", info.Install)
	row("Binary", pathLink(t, info.Binary))
	row("Config", pathLink(t, info.Config))
	row("Cache", pathLink(t, info.Cache))
}

func pathLink(t out.Theme, path string) string {
	if path == "" {
		return ""
	}
	return t.Link(out.Tilde(path), path)
}

func describeBuild() selfupdate.Build {
	info, _ := debug.ReadBuildInfo()
	return selfupdate.Describe(version, date, info)
}
