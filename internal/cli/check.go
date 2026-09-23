package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

type checkResult struct {
	Problems []*out.Error `json:"problems"`
}

func (a *app) checkCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:         "check",
		Annotations: reads(),
		Short:       "Fail when the lock is stale, a locked file can't be fetched, or a mod's dependencies aren't met",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			res := checkResult{Problems: []*out.Error{}}
			problem := func(err error) {
				e := out.AsError(err)
				a.printer.Report(e)
				res.Problems = append(res.Problems, e)
			}
			if diffs := p.LockDifferences(); len(diffs) > 0 {
				e := out.Errorf("lock-stale", "shulker.lock does not match shulker.json")
				e.Help = "run `shulker lock`"
				e.Items = diffs
				problem(e)
			}
			errs, warnings := a.checkLockedFiles(cmd.Context(), p)
			warnings = append(p.GoneFiles(d.cache.Has), warnings...)
			for _, err := range errs {
				problem(err)
			}
			a.warn(warnings)
			if strict && len(warnings) > 0 {
				e := out.Errorf("strict-warnings", "%d warning(s), and --strict fails on any", len(warnings))
				e.Items = warnings
				problem(e)
			}
			if len(res.Problems) > 0 {
				return checkFailed(res)
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.OK("no problems found", "")
			})
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "fail on warnings too")
	a.registerFailFast(cmd)
	return cmd
}

// checkLockedFiles fetches every locked file into the cache and validates every declared side
// with all their jars read.
func (a *app) checkLockedFiles(ctx context.Context, p *project.Project) ([]error, []string) {
	r, err := a.resolver(ctx, p)
	if err != nil {
		return []error{err}, nil
	}
	var errs []error
	_, warnings, fetchErr := r.Install(ctx)
	if joined, ok := fetchErr.(interface{ Unwrap() []error }); ok {
		errs = append(errs, joined.Unwrap()...)
	} else if fetchErr != nil {
		errs = append(errs, fetchErr)
	}
	v, err := r.Validate()
	if err != nil {
		return append(errs, err), warnings
	}
	if err := v.Err(); err != nil {
		errs = append(errs, err)
	}
	for _, w := range v.Warnings {
		// Only a file the fetch failed on can be missing here, and that failure is already a problem.
		if fetchErr == nil || !slices.Contains(v.Undownloaded, w) {
			warnings = append(warnings, w)
		}
	}
	return errs, warnings
}

// checkFailed is the run's error: one row per problem found, each already reported in full above
// it, with every problem's own items listed together for CI to annotate.
func checkFailed(res checkResult) error {
	e := out.Errorf("check-failed", "%d problem(s) found", len(res.Problems))
	e.Data = res
	for _, p := range res.Problems {
		message, _, _ := strings.Cut(p.Message, "\n")
		e.Rows = append(e.Rows, out.Detail{Label: p.Code, Text: strings.TrimSuffix(message, ":")})
		if len(p.Items) == 0 {
			e.Items = append(e.Items, fmt.Sprintf("%s: %s", p.Code, message))
		}
		for _, item := range p.Items {
			e.Items = append(e.Items, fmt.Sprintf("%s: %s", p.Code, item))
		}
	}
	return e
}
