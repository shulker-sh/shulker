package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

var (
	checkScopes        = []string{"lock", "files", "deps", "server"}
	defaultCheckScopes = []string{"lock", "files", "deps"}
)

type checkResult struct {
	Scopes   []string     `json:"scopes"`
	Problems []*out.Error `json:"problems"`
}

func (a *app) checkCmd() *cobra.Command {
	var strict, all bool
	cmd := &cobra.Command{
		Use:         "check [lock|files|deps|server]...",
		Annotations: reads(),
		Short:       "Fail when the lock is stale, a locked file can't be fetched, or a mod's dependencies aren't met",
		ValidArgs:   checkScopes,
		RunE: func(cmd *cobra.Command, args []string) error {
			scopes, err := checkScopesFor(args, all)
			if err != nil {
				return err
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if slices.Contains(scopes, "server") && !p.Manifest.HasSide("server") {
				if !all {
					return project.NoSide("server")
				}
				scopes = slices.DeleteFunc(scopes, func(s string) bool { return s == "server" })
			}
			res := checkResult{Scopes: scopes, Problems: []*out.Error{}}
			problem := func(err error) {
				e := out.AsError(err)
				a.printer.Report(e)
				res.Problems = append(res.Problems, e)
			}
			if slices.Contains(scopes, "lock") {
				if diffs := p.LockDifferences(); len(diffs) > 0 {
					e := out.Errorf("lock-stale", "shulker.lock does not match shulker.json")
					e.Help = "run `shulker lock`"
					e.Items = diffs
					problem(e)
				}
			}
			errs, warnings := a.checkLocked(cmd.Context(), p, scopes)
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
				l.OK("no problems found", "checked "+strings.Join(scopes, ", "))
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "run every check, server included when the project declares one")
	cmd.Flags().BoolVar(&strict, "strict", false, "fail on warnings too")
	a.registerFailFast(cmd)
	return cmd
}

// checkScopesFor is the checks to run, in checkScopes' order: the ones named, every one with all,
// and the default set with neither.
func checkScopesFor(args []string, all bool) ([]string, error) {
	if all && len(args) > 0 {
		return nil, out.Errorf("usage", "--all runs every check; name checks or pass --all, not both")
	}
	for _, arg := range args {
		if !slices.Contains(checkScopes, arg) {
			e := out.Errorf("usage", "%q is not a check", arg)
			e.Candidates, e.Given = checkScopes, arg
			return nil, e
		}
	}
	switch {
	case all:
		return slices.Clone(checkScopes), nil
	case len(args) == 0:
		return slices.Clone(defaultCheckScopes), nil
	}
	return slices.DeleteFunc(slices.Clone(checkScopes), func(s string) bool { return !slices.Contains(args, s) }), nil
}

// checkLocked runs the scopes that need the locked files: files fetches every one into the cache,
// deps fetches the mod jars and validates every declared side with them all read, and server fetches
// the server jar and its Java runtime.
func (a *app) checkLocked(ctx context.Context, p *project.Project, scopes []string) ([]error, []string) {
	files, deps, srv := slices.Contains(scopes, "files"), slices.Contains(scopes, "deps"), slices.Contains(scopes, "server")
	if !files && !deps && !srv {
		return nil, nil
	}
	d, err := a.deps()
	if err != nil {
		return []error{err}, nil
	}
	var warnings []string
	if files {
		warnings = p.GoneFiles(d.Cache.Has)
	}
	r, err := a.resolver(ctx, p)
	if err != nil {
		return []error{err}, warnings
	}
	var fetchErr error
	switch {
	case files:
		var dropWarnings []string
		_, dropWarnings, fetchErr = r.Install(ctx)
		warnings = append(warnings, dropWarnings...)
	case deps:
		_, _, fetchErr = r.InstallMods(ctx)
	}
	errs := splitJoined(fetchErr)
	if srv {
		srvErrs, srvWarnings := a.checkServer(ctx, p, r)
		errs, warnings = append(errs, srvErrs...), append(warnings, srvWarnings...)
	}
	if !deps {
		return errs, warnings
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

// checkServer fetches the server jar and, unless shulker.json names a Java of its own, the managed
// runtime the server runs on. A server jar the lock doesn't record yet is fetched but not locked:
// check writes no lock.
func (a *app) checkServer(ctx context.Context, p *project.Project, r *resolve.Resolver) ([]error, []string) {
	d, err := a.deps()
	if err != nil {
		return []error{err}, nil
	}
	var errs []error
	var warnings []string
	if _, err := r.EnsureServerJar(ctx, d.meta); err != nil {
		errs = append(errs, err)
	}
	if p.Manifest.Java != "" {
		return errs, nil
	}
	_, err = a.freshestJava(ctx, p, serverJavaFix)
	switch {
	case out.CodeOf(err) == "runtime-unavailable":
		warnings = append(warnings, runtimeWarning(err))
	case err != nil:
		errs = append(errs, err)
	}
	return errs, warnings
}

// checkFailed is the run's error: one row per problem found, each already reported in full above
// it, with every problem's own items listed together for CI to annotate.
func checkFailed(res checkResult) error {
	e := out.Errorf("check-failed", "%d problem(s) found", len(res.Problems))
	e.Data, e.IsSummary = res, true
	for _, p := range res.Problems {
		headline := p.Headline()
		e.Rows = append(e.Rows, out.Detail{Label: p.Code, Text: headline})
		if len(p.Items) == 0 {
			e.Items = append(e.Items, fmt.Sprintf("%s: %s", p.Code, headline))
		}
		for _, item := range p.Items {
			e.Items = append(e.Items, fmt.Sprintf("%s: %s", p.Code, item))
		}
	}
	return e
}
