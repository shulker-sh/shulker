package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/audit"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/security"
)

func (a *app) auditExposureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "exposure",
		Annotations: reads(),
		Short:       "Map who can change what the project runs",
		Long:        "Map who can change what the project, or an instance with -i, runs: for each lock entry, who controls it (a provider listing's author, a source's author, or you) and how it changes (pinned, on update, with its source, or when you edit the file), and whether it changes on its own at launch because the pre-launch hook or the Mojang shim syncs first. It also lists the override folders and the files they lay, the memory, jvmArgs and wrapper each instance launches with and where each comes from, and the protections in force. It reads nothing from the network.",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, err := a.auditTarget(cmd)
			if err != nil {
				return err
			}
			var instances []project.InstanceEntry
			if scope.instance != nil {
				instances = []project.InstanceEntry{*scope.instance}
			} else {
				entries, err := a.loadInstanceEntries()
				if err != nil {
					return err
				}
				instances = audit.InstancesOf(scope.builder, entries)
			}
			path, err := a.configFile()
			if err != nil {
				return err
			}
			cfg, err := config.LoadFile(path)
			if err != nil {
				return err
			}
			x, err := audit.Expose(scope.builder, audit.ExposureOptions{Instances: instances, Play: cfg.Play.LaunchSettings, PackMemory: scope.project.ClientMemory(), Protections: security.Protections(cfg.Security.ReleaseAge())})
			if err != nil {
				return err
			}
			return a.printer.Emit(x, func(l *out.Lines) { printExposure(l, x, scope.builder.Providers) })
		},
	}
	a.scopeFlags(cmd)
	return cmd
}

func printExposure(l *out.Lines, x *audit.Exposure, ps provider.Providers) {
	atLaunch := 0
	for _, e := range x.Entries {
		if e.AtLaunch {
			atLaunch++
		}
	}
	if len(x.Launches) == 0 {
		l.Info("No registered instance builds this project, so nothing in it changes on its own at a launch.")
	}
	for _, launch := range x.Launches {
		switch by := launchSync(launch); {
		case by == "":
			l.Info("Nothing syncs " + launch.Instance + " before a launch, so nothing in it changes on its own when the game starts.")
		case atLaunch == 0:
			l.Info(by + " syncs " + launch.Instance + " before every launch, and nothing in it follows a source.")
		default:
			l.Warn(fmt.Sprintf("%s syncs %s before every launch, so %s on %s own when the game starts.", by, launch.Instance, out.Count(atLaunch, "entry changes", "entries change"), countWord(atLaunch == 1, "its", "their")))
		}
	}
	if len(x.Entries) > 0 {
		l.Blank()
		l.Heading("Entries")
		var rows [][]string
		for _, e := range x.Entries {
			kind := e.Type
			if e.Modpack != "" {
				kind += " from " + e.Modpack
			}
			rows = append(rows, []string{e.Key, kind, controller(e.Control, ps), changes(e.Control)})
		}
		l.Table([]string{"Entry", "Type", "Controlled by", "Changes"}, rows, out.Columns(l.T.StyleCommand(), l.T.Style()))
	}
	if len(x.Overrides) > 0 {
		l.Blank()
		l.Heading("Override folders")
		var rows []out.Row
		for _, o := range x.Overrides {
			rows = append(rows, out.Row{Label: o.Folder, Text: fmt.Sprintf("%s, changes %s, lays %s", controller(o.Control, ps), changes(o.Control), out.Count(len(o.Files), "file", "files")), Children: o.Files})
		}
		l.Tree(rows...)
	}
	for _, launch := range x.Launches {
		l.Blank()
		l.Heading("Launch settings for " + launch.Instance)
		var settings [][]string
		for _, s := range launch.Settings {
			value, from := "(unset)", "-"
			if s.From != "" {
				value, from = settingValue(s.Value), s.From
			}
			settings = append(settings, []string{s.Key, value, from})
		}
		l.Table([]string{"Setting", "Value", "From"}, settings, out.Columns(l.T.StyleCommand(), l.T.Style()))
	}
	on := 0
	for _, p := range x.Protections {
		if p.On {
			on++
		}
	}
	l.Blank()
	l.Info(fmt.Sprintf("%d of %s on", on, out.Count(len(x.Protections), "protection", "protections")))
	l.Nudge(security.Nudge.Lead, security.Nudge.Command)
}

// launchSync names what syncs the instance before each launch, empty when nothing does.
func launchSync(launch audit.Launch) string {
	switch launch.SyncsBy {
	case audit.ByHook:
		return "The " + launcher.Title(launch.Launcher) + " pre-launch hook"
	case audit.ByShim:
		return "The Mojang shim"
	case audit.ByPlay:
		return "Shulker"
	}
	return ""
}

func controller(c audit.Control, ps provider.Providers) string {
	switch c.Owner {
	case audit.OwnerProvider:
		return ps.Title(c.Provider) + " project " + c.Project
	case audit.OwnerSource:
		return c.Source
	}
	return "you"
}

func changes(c audit.Control) string {
	switch c.Changes {
	case audit.Pinned:
		return "pinned"
	case audit.Floating:
		return "on update"
	case audit.Local:
		return "when you edit it"
	case audit.Follows:
		if c.AtLaunch {
			return "at every launch"
		}
		return "on every sync"
	}
	return c.Changes
}

func settingValue(v any) string {
	if list, ok := v.([]string); ok {
		if len(list) == 0 {
			return "(none)"
		}
		return strings.Join(list, " ")
	}
	return fmt.Sprint(v)
}
