package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
)

// savesWhere is the selection backup, restore, saves and saves prune share: the current directory,
// -C or -i, a save group by --group, or every registered row with --all, narrowed by --launcher
// and --side.
type savesWhere struct {
	group string
	sel   instanceSelection
}

func (s *savesWhere) register(cmd *cobra.Command, group, all string) {
	cmd.Flags().StringVar(&s.group, "group", "", group)
	s.sel.register(cmd, all+" (narrow with --launcher or --side)")
}

// savesPick is one target a fanned-out saves command acts on, and the rows that reach it. err is
// why the rows' target couldn't be resolved.
type savesPick struct {
	savesTarget
	rows []instanceEntry
	err  error
}

// savesTargetFor resolves the one target a command acts on without --all.
func (a *app) savesTargetFor(w savesWhere) (savesTarget, error) {
	if !w.sel.narrows() {
		return a.savesTargetOf(w.group)
	}
	if err := w.sel.check(); err != nil {
		return savesTarget{}, err
	}
	if a.instance == "" || w.group != "" {
		return savesTarget{}, out.Errorf("usage", "--launcher and --side narrow -i or --all")
	}
	if a.dir != "" {
		return savesTarget{}, out.Errorf("usage", "pass -C or -i, not both: -i already says which directory to act on")
	}
	entries, err := a.selectInstances(a.instance, w.sel)
	if err != nil {
		return savesTarget{}, err
	}
	return a.savesTargetAt(entries[0].Dir)
}

// savesPicks is every target --all reaches: one per row the selection admits, except that the rows
// in one save group share a single target. A group reached by one row still names that instance;
// one reached by several names none, as --group does.
func (a *app) savesPicks(w savesWhere) ([]savesPick, error) {
	if w.group != "" {
		return nil, out.Errorf("usage", "pass --group or --all, not both: --group names one save group, --all every instance")
	}
	entries, err := a.selectInstances(a.instance, w.sel)
	if err != nil {
		return nil, err
	}
	var picks []savesPick
	for _, e := range entries {
		t, err := a.savesTargetAt(e.Dir)
		if err != nil {
			picks = append(picks, savesPick{rows: []instanceEntry{e}, err: err})
			continue
		}
		t.via = e.ID
		i := slices.IndexFunc(picks, func(p savesPick) bool { return p.err == nil && p.backups == t.backups })
		if i < 0 {
			picks = append(picks, savesPick{savesTarget: t, rows: []instanceEntry{e}})
			continue
		}
		picks[i].rows = append(picks[i].rows, e)
		picks[i].Dir, picks[i].via = "", ""
	}
	return picks, nil
}

// onSaves runs one saves command on the target w selects, or with --all on each target it reaches.
func onSaves[T any](a *app, w savesWhere, failed func(n, of int) *out.Error, run func(savesTarget) (T, error), print func(T, *out.Lines), skip ...string) error {
	if w.sel.all {
		picks, err := a.savesPicks(w)
		if err != nil {
			return err
		}
		return eachTarget(a, picks, failed, run, print, skip...)
	}
	target, err := a.savesTargetFor(w)
	if err != nil {
		return err
	}
	res, err := run(target)
	if err != nil {
		return err
	}
	return a.printer.Emit(res, func(l *out.Lines) { print(res, l) })
}

type savesRun[T any] struct {
	savesTarget
	Instances []string   `json:"instances"`
	OK        bool       `json:"ok"`
	Skipped   string     `json:"skipped,omitempty"`
	Result    *T         `json:"result,omitempty"`
	Error     *out.Error `json:"error,omitempty"`
}

// eachTarget runs one saves command over every pick, printing each as it finishes. A target that
// fails with one of the skip codes has nothing to act on, and is reported without failing the run;
// any other failure fails it with failed's error once every target has run.
func eachTarget[T any](a *app, picks []savesPick, failed func(n, of int) *out.Error, run func(savesTarget) (T, error), print func(T, *out.Lines), skip ...string) error {
	lines := a.printer.Out()
	runs := []savesRun[T]{}
	failures := 0
	for i, p := range picks {
		if !a.printer.JSON {
			if i > 0 {
				lines.Blank()
			}
			lines.Heading(pickHeading(lines.T, p))
		}
		r := savesRun[T]{savesTarget: p.savesTarget, Instances: instanceIDs(p.rows), OK: true}
		err := p.err
		if err == nil {
			restore := a.scopeWarnings(pickLabel(p))
			var res T
			res, err = run(p.savesTarget)
			restore()
			if err == nil {
				r.Result = &res
				if !a.printer.JSON {
					print(res, lines)
				}
			}
		}
		if err != nil {
			e := out.AsError(err)
			if slices.Contains(skip, e.Code) {
				r.Skipped = e.Message
				if !a.printer.JSON {
					lines.Info(e.Message)
				}
			} else {
				failures++
				r.OK, r.Error = false, e
				if !a.printer.JSON {
					a.printer.Err().Error(e)
				}
			}
		}
		runs = append(runs, r)
	}
	if failures > 0 {
		e := failed(failures, len(picks))
		e.Data = runs
		return e
	}
	return a.printer.Emit(runs, func(*out.Lines) {})
}

func pickHeading(t out.Theme, p savesPick) string {
	if len(p.rows) == 1 {
		return instanceHeading(t, p.rows[0])
	}
	return t.Bold(p.Group) + " " + t.Grey("save group") + t.Aside(strings.Join(instanceIDs(p.rows), ", "))
}

func pickLabel(p savesPick) string {
	if len(p.rows) == 1 {
		return p.rows[0].Label()
	}
	return p.Group
}
