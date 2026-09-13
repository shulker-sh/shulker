package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"shulker.sh/shulker/internal/out"
)

const (
	docsURL    = "https://shulker.sh/docs"
	agentsURL  = "https://shulker.sh/llms.txt"
	helpIndent = "  "
)

var helpGroups = []struct {
	ID, Title string
	Commands  []string
}{
	{"project", "Project", []string{"init", "get", "set", "unset", "lock", "import", "export"}},
	{"mods", "Mods", []string{"add", "remove", "update", "outdated", "pin", "unpin", "ignore", "unignore", "suggests"}},
	{"builds", "Targets and builds", []string{"target", "install", "build", "diff", "pull", "feature"}},
	{"launchers", "Launchers", []string{"link", "links", "sync", "unlink"}},
	{"servers", "Packs and servers", []string{"pack", "serve", "player"}},
	{"shulker", "Shulker", []string{"config", "self", "version", "completion"}},
}

// installHelp files the root's commands into groups and renders every --help
// through the theme instead of cobra's template.
func (a *app) installHelp(root *cobra.Command) {
	byName := map[string]string{}
	for _, g := range helpGroups {
		root.AddGroup(&cobra.Group{ID: g.ID, Title: g.Title})
		for _, name := range g.Commands {
			byName[name] = g.ID
		}
	}
	for _, c := range root.Commands() {
		c.GroupID = byName[c.Name()]
	}
	root.SetCompletionCommandGroupID("shulker")
	root.InitDefaultHelpCmd()
	for _, c := range root.Commands() {
		if c.Name() == "help" {
			c.Hidden = true
		}
	}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) { a.help(cmd) })
	root.SetUsageFunc(func(cmd *cobra.Command) error { a.help(cmd); return nil })
}

func (a *app) help(cmd *cobra.Command) {
	l := &out.Lines{W: cmd.OutOrStdout(), T: a.printer.Theme}
	t := l.T
	l.Text(t.Command(cmd.CommandPath()) + "  " + cmd.Short)
	l.Blank()
	l.Heading("Usage")
	usage := cmd.UseLine()
	if cmd.HasAvailableSubCommands() {
		usage = cmd.CommandPath() + " <command> [flags]"
	}
	l.Text(helpIndent + t.Grey("$") + " " + t.Command(usage))
	l.Blank()
	if !cmd.HasParent() {
		helpRoot(l, cmd)
	} else if cmd.HasAvailableSubCommands() {
		helpCommands(l, "Commands", cmd.Commands(), helpWidth(cmd.Commands()))
	}
	local, global := cmd.NonInheritedFlags(), cmd.InheritedFlags()
	if !cmd.HasParent() {
		local, global = global, local
	}
	helpFlags(l, "Flags", local)
	helpFlags(l, "Global flags", global)
	if cmd.Long != "" {
		for _, line := range strings.Split(strings.TrimRight(cmd.Long, "\n"), "\n") {
			l.Text(t.Grey(line))
		}
		l.Blank()
	}
	l.Text(t.Grey("Docs") + " " + t.Link(t.Cyan(docsURL), docsURL))
	l.Text(t.Grey("For agents") + " " + t.Link(t.Cyan(agentsURL), agentsURL))
}

func helpRoot(l *out.Lines, root *cobra.Command) {
	width := helpWidth(root.Commands())
	grouped := map[string]bool{}
	for _, g := range helpGroups {
		var cmds []*cobra.Command
		for _, name := range g.Commands {
			for _, c := range root.Commands() {
				if c.Name() == name {
					cmds = append(cmds, c)
					grouped[name] = true
				}
			}
		}
		helpCommands(l, g.Title, cmds, width)
	}
	var rest []*cobra.Command
	for _, c := range root.Commands() {
		if !grouped[c.Name()] {
			rest = append(rest, c)
		}
	}
	helpCommands(l, "Other commands", rest, width)
}

func helpWidth(cmds []*cobra.Command) int {
	width := 0
	for _, c := range cmds {
		if c.IsAvailableCommand() {
			width = max(width, len(c.Name()))
		}
	}
	return width
}

func helpCommands(l *out.Lines, title string, cmds []*cobra.Command, width int) {
	t := l.T
	shown := false
	for _, c := range cmds {
		if !c.IsAvailableCommand() {
			continue
		}
		if !shown {
			l.Heading(title)
			shown = true
		}
		l.Text(helpIndent + t.Command(c.Name()) + strings.Repeat(" ", width-len(c.Name())+2) + c.Short)
	}
	if shown {
		l.Blank()
	}
}

type helpFlag struct {
	head, usage, aside string
	width              int
}

func helpFlags(l *out.Lines, title string, flags *pflag.FlagSet) {
	t := l.T
	var rows []helpFlag
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" || f.Hidden {
			return
		}
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", " + name
		}
		kind, usage := pflag.UnquoteUsage(f)
		kind = strings.TrimSuffix(strings.TrimSuffix(kind, "Array"), "Slice")
		row := helpFlag{head: t.Command(name), usage: usage, width: len(name)}
		if kind != "" {
			row.head += " " + t.Grey("<"+kind+">")
			row.width += len(kind) + 3
		}
		switch def := strings.Trim(f.DefValue, "[]"); def {
		case "", "false", "0":
		default:
			row.aside = t.Aside("default: " + def)
		}
		rows = append(rows, row)
	})
	if len(rows) == 0 {
		return
	}
	width := 0
	for _, r := range rows {
		width = max(width, r.width)
	}
	l.Heading(title)
	for _, r := range rows {
		l.Text(helpIndent + r.head + strings.Repeat(" ", width-r.width+2) + r.usage + r.aside)
	}
	l.Blank()
}
