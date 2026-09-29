package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
)

func (a *app) registerFailFast(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&a.failFast, "fail-fast", false, "stop at the first file that fails to download, rather than trying them all.")
}

// registerEveryFetch gives cmd the -v that keeps a line for every file a run of fetches gets,
// where the run would otherwise settle into one count.
func (a *app) registerEveryFetch(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&a.everyFetch, "verbose", "v", false, "print a line for every file fetched, rather than one count per group.")
}

// splitJoined is the errors an errors.Join holds, err alone when it is any other error, and none
// for nil.
func splitJoined(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}
	if err == nil {
		return nil
	}
	return []error{err}
}

// lastOf reports each of several joined errors but the last, which it returns for the run to fail
// with, carrying the others in its data for --json.
func (a *app) lastOf(err error) error {
	errs := splitJoined(err)
	if len(errs) == 1 {
		return errs[0]
	}
	var earlier []*out.Error
	for _, e := range errs[:len(errs)-1] {
		earlier = append(earlier, out.AsError(e))
		a.printer.Report(earlier[len(earlier)-1])
	}
	last := out.AsError(errs[len(errs)-1])
	if last.Data == nil {
		last.Data = map[string]any{"errors": earlier}
	}
	return last
}
