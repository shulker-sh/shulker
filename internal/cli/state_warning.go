package cli

import (
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/schema"
)

// warnState warns that a directory's state file was read as empty, with the command that deals with
// it beneath: `shulker self update` for one a newer shulker wrote, otherwise force, the command
// that takes the directory's files over. An empty force, as on a run that already forces, leaves
// the warning without one.
func (a *app) warnState(e *instance.StateError, force string) {
	switch {
	case e == nil:
	case e.Newer:
		a.printer.WarnNudge(schema.UpdateNudge, "%s", e)
	case force == "":
		a.printer.Warn("%s", e)
	default:
		a.printer.WarnNudge(out.Nudge{Lead: "Take them over", Command: force}, "%s", e)
	}
}

// warnBuild is warnFor for a build's report, its state warning last so its nudge sits under the
// warnings rather than between them.
func (a *app) warnBuild(side string, several bool, warnings []string, state *instance.StateError, force string) {
	if several {
		defer a.scopeWarnings(side)()
	}
	a.warn(warnings)
	a.warnState(state, force)
}

// forceCommand is the command that rebuilds dir with --force: p's own build for its build
// directory, sync for a registered instance by id, and otherwise sync into dir from the source its
// instance file records.
func (a *app) forceCommand(p *project.Project, side, dir string) string {
	if p != nil && config.SameDir(dir, filepath.Join(p.Dir, p.Manifest.BuildDir(side))) {
		command := "shulker build"
		if len(p.Manifest.Sides()) > 1 {
			command += " " + side
		}
		return command + " --force"
	}
	if entries, err := a.loadInstanceEntries(); err == nil {
		for _, e := range entries {
			if config.SameDir(e.Dir, dir) {
				return "shulker sync -i " + e.ID + " --force"
			}
		}
	}
	return "shulker sync --into " + account.QuoteName(dir) + " --force"
}

// warnKeptConflicts warns about the files a launch kept as the player had them, where the pack
// changed them too, with the command that takes the pack's version beneath. The instance's own sync
// comes first: a launcher runs the hook from anywhere, and it works from anywhere. A directory that
// is a project's own build can also keep the player's version for good, with pull.
func (a *app) warnKeptConflicts(kept []string, p *project.Project, side, dir string) {
	if len(kept) == 0 {
		return
	}
	id := a.instanceID(dir)
	force := "shulker sync -i " + id + " --force"
	if id == "" {
		force = a.forceCommand(p, side, dir)
	}
	keep := ""
	if p != nil && config.SameDir(dir, filepath.Join(p.Dir, p.Manifest.BuildDir(side))) {
		keep = ", `shulker pull` keeps yours"
	}
	a.printer.WarnNudge(out.Nudge{Lead: "Take the pack's version", Command: force}, "kept your version of %s; `shulker diff` shows the pack's%s", strings.Join(kept, ", "), keep)
}

// rerunForced is the command line that ran cmd, with --force added. Flags that only change how
// output looks are left off.
func rerunForced(cmd *cobra.Command, args []string) string {
	parts := []string{cmd.CommandPath()}
	for _, arg := range args {
		parts = append(parts, account.QuoteName(arg))
	}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		switch f.Name {
		case "force", "json", "no-color", "ascii":
			return
		}
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand
		}
		if items, ok := f.Value.(pflag.SliceValue); ok {
			for _, item := range items.GetSlice() {
				parts = append(parts, name, account.QuoteName(item))
			}
			return
		}
		if f.Value.Type() == "bool" {
			if f.Value.String() != "true" {
				name += "=" + f.Value.String()
			}
			parts = append(parts, name)
			return
		}
		parts = append(parts, name, account.QuoteName(f.Value.String()))
	})
	return strings.Join(append(parts, "--force"), " ")
}

// takeOver is the take-over command for a run of cmd: none when it already forces.
func takeOver(cmd *cobra.Command, args []string, force bool) string {
	if force {
		return ""
	}
	return rerunForced(cmd, args)
}

// syncTakeOver is the take-over command for a sync into dir: the sync command's own line when it
// ran one, otherwise the command that rebuilds dir.
func (a *app) syncTakeOver(req syncRequest, p *project.Project, side, dir string) string {
	switch {
	case req.Force:
		return ""
	case req.rerun != "":
		return req.rerun
	}
	return a.forceCommand(p, side, dir)
}
