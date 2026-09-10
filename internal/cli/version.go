package cli

import (
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"
)

type versionInfo struct {
	Version string `json:"version"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the shulker version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := versionInfo{Version: version, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
			return a.printer.Emit(info, func(w io.Writer) {
				fmt.Fprintf(w, "shulker %s (%s %s/%s)\n", info.Version, info.Go, info.OS, info.Arch)
			})
		},
	}
}
