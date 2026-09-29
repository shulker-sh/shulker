package cli

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/audit"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
)

func (a *app) auditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "audit [key...]",
		Annotations: reads(),
		Short:       "Report lock entries and files that deserve a closer look",
		Long:        "Report what deserves a closer look in the project, or an instance with -i: locked files their provider no longer has, lock entries that download from outside their provider or that it files under another project, files no provider published, jars in mods/ that no longer match the lock or that it doesn't name, and versions younger than security.minReleaseAge. It fails for files gone from their provider and entries from outside it, so CI can gate on it. Name keys to audit only those entries and the ones each named modpack brings.",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, dirs, err := a.auditTarget(cmd)
			if err != nil {
				return err
			}
			age, err := a.minReleaseAge()
			if err != nil {
				a.printer.Warn("couldn't read security.minReleaseAge, using its default: %v.", err)
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			rep, err := audit.Run(cmd.Context(), b, audit.Options{Keys: args, Dirs: dirs, MinReleaseAge: age, Now: env.Clock(d.Now)})
			if err != nil {
				return err
			}
			if !rep.Fails() {
				return a.printer.Emit(rep, func(l *out.Lines) { printAudit(l, rep, b.Providers) })
			}
			if !a.printer.JSON {
				printAudit(a.printer.Out(), rep, b.Providers)
			}
			return auditFailed(rep)
		},
	}
	a.scopeFlags(cmd)
	return cmd
}

// auditTarget is the builder over what the audit reads, and the directories whose mods/ it checks:
// the project and the directories its sides were built into, or with -i the instance and the
// project it runs on.
func (a *app) auditTarget(cmd *cobra.Command) (*build.Builder, []audit.Dir, error) {
	if a.instance == "" {
		p, err := a.openProject()
		if err != nil {
			return nil, nil, err
		}
		b, err := a.auditBuilder(cmd, p)
		if err != nil {
			return nil, nil, err
		}
		return b, audit.ProjectDirs(b), nil
	}
	entry, err := a.scopeInstance()
	if err != nil {
		return nil, nil, err
	}
	d, err := a.deps()
	if err != nil {
		return nil, nil, err
	}
	p, dir, err := audit.Instance(d.Cache, entry)
	if err != nil {
		return nil, nil, err
	}
	b, err := a.auditBuilder(cmd, p)
	if err != nil {
		return nil, nil, err
	}
	return b, []audit.Dir{dir}, nil
}

func (a *app) auditBuilder(cmd *cobra.Command, p *project.Project) (*build.Builder, error) {
	if err := p.RequireLock(); err != nil {
		return nil, err
	}
	return a.builder(cmd.Context(), p)
}

// auditFailed sums up what failed the audit, each already printed in full above it.
func auditFailed(rep *audit.Report) error {
	var parts []string
	if n := len(rep.Takedowns); n > 0 {
		parts = append(parts, goneHeadline(n))
	}
	if n := len(rep.Provenance) + len(rep.Moved); n > 0 {
		parts = append(parts, provenanceHeadline(n))
	}
	e := out.Errorf("audit-failed", "%s", strings.Join(parts, "; "))
	e.Data, e.IsSummary = rep, true
	e.Items = failedKeys(rep)
	return e
}

func failedKeys(rep *audit.Report) []string {
	var keys []string
	for _, f := range rep.Takedowns {
		keys = append(keys, f.Key)
	}
	for _, m := range rep.Provenance {
		keys = append(keys, m.Key)
	}
	for _, f := range rep.Moved {
		keys = append(keys, f.Key)
	}
	return keys
}

func goneHeadline(n int) string {
	return out.Count(n, "file", "files") + " gone from " + countWord(n == 1, "its", "their") + " provider"
}

func provenanceHeadline(n int) string {
	return out.Count(n, "entry doesn't", "entries don't") + " come from where the lock says"
}

func printAudit(l *out.Lines, rep *audit.Report, ps provider.Providers) {
	gap := func() {}
	section := func() {
		gap()
		gap = l.Blank
	}
	if len(rep.Takedowns)+len(rep.Provenance)+len(rep.Moved)+len(rep.Unpublished)+len(rep.Installed)+len(rep.Young) == 0 {
		section()
		checked := "checked takedowns, provenance, unpublished files, installed jars, release age"
		if len(rep.Skipped) > 0 {
			checked = "checked provenance, unpublished files, installed jars, release age"
		}
		l.OK("No problems found", checked)
	}
	for _, sk := range rep.Skipped {
		section()
		l.Info("Skipped the takedown check for " + ps.Title(sk.Provider) + ": " + sk.Reason)
	}
	if n := len(rep.Takedowns); n > 0 {
		section()
		l.Failed(goneHeadline(n))
		var rows []out.Row
		var keys []string
		for _, f := range rep.Takedowns {
			rows = append(rows, out.Row{Label: f.Key, Text: ps.Title(f.Provider) + " no longer has the locked version, " + cmp.Or(f.Number, f.Version)})
			keys = append(keys, f.Key)
		}
		l.Tree(rows...)
		l.Blank()
		l.Paragraph("A file gone from its provider is a reason to look, not proof: authors delete their own old versions too.")
		named := strings.Join(keys, " ")
		l.Nudge("Look at "+countWord(n == 1, "it", "them"), "shulker audit "+named)
		l.Nudge("Move off "+countWord(n == 1, "it", "them"), "shulker update "+named)
		l.Nudge("Or remove "+countWord(n == 1, "it", "them"), "shulker remove "+named)
	}
	if n := len(rep.Provenance) + len(rep.Moved); n > 0 {
		section()
		l.Failed(provenanceHeadline(n))
		var rows []out.Row
		var keys []string
		for _, m := range rep.Provenance {
			rows = append(rows, out.Row{Label: m.Key, Text: fmt.Sprintf("locked from %s, downloads from %s", ps.Title(m.Provider), m.Host)})
			keys = append(keys, m.Key)
		}
		for _, f := range rep.Moved {
			rows = append(rows, out.Row{Label: f.Key, Text: fmt.Sprintf("%s files it under project %s, not %s", ps.Title(f.Provider), f.FiledUnder, f.Project)})
			keys = append(keys, f.Key)
		}
		l.Tree(rows...)
		l.Nudge("Look "+countWord(n == 1, "it", "them")+" up again from "+countWord(n == 1, "its", "their")+" provider", "shulker lock "+strings.Join(keys, " "))
	}
	if n := len(rep.Unpublished); n > 0 {
		section()
		l.Info(out.Count(n, "file", "files") + " no provider published")
		var rows []out.Row
		for _, u := range rep.Unpublished {
			rows = append(rows, out.Row{Label: u.Path, Text: unpublishedOrigin(u)})
		}
		l.Tree(rows...)
	}
	if n := len(rep.Installed); n > 0 {
		section()
		l.Warn(out.Count(n, "jar", "jars") + " in mods/ " + countWord(n == 1, "doesn't", "don't") + " match the lock")
		var rows []out.Row
		changed := false
		dirs := map[string]bool{}
		for _, in := range rep.Installed {
			dirs[in.Dir] = true
		}
		for _, in := range rep.Installed {
			text := "not in the lock"
			if in.Problem == audit.Changed {
				text, changed = "changed since shulker placed it", true
			}
			if in.Key != "" {
				text += " (" + in.Key + ")"
			}
			if len(dirs) > 1 {
				text += " in " + in.Dir
			}
			rows = append(rows, out.Row{Label: in.Path, Text: text})
		}
		l.Tree(rows...)
		if changed {
			l.Nudge("Put the locked copies back", "shulker build --force")
		}
	}
	if n := len(rep.Young); n > 0 {
		section()
		l.Warn(fmt.Sprintf("%s younger than %s", out.Count(n, "version", "versions"), out.Count(rep.MinReleaseAge, "day", "days")))
		var rows []out.Row
		for _, y := range rep.Young {
			rows = append(rows, out.Row{Label: y.Key, Text: y.Version + " (" + y.Text() + ")"})
		}
		l.Tree(rows...)
	}
}

func unpublishedOrigin(u audit.Unpublished) string {
	var text string
	switch u.From {
	case audit.FromFile:
		text = "local file " + u.Source
	case audit.FromDownload:
		text = "downloaded from " + u.Source
	default:
		text = "laid by " + u.Source
	}
	if u.Key != "" {
		text = u.Key + ", " + text
	}
	return text
}

// countWord is singular for one of something and plural otherwise.
func countWord(one bool, singular, plural string) string {
	if one {
		return singular
	}
	return plural
}
