package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"shulker.sh/shulker/internal/docs"
	"shulker.sh/shulker/internal/out"
)

const (
	docsURL     = "https://shulker.sh/docs"
	agentsURL   = "https://shulker.sh/llms.txt"
	helpIndent  = "  "
	helpColumns = 80
)

var helpGroups = []struct {
	ID, Title string
	Commands  []string
}{
	{"project", "Project", []string{"init", "create", "get", "set", "unset", "lock", "check", "import", "export"}},
	{"mods", "Mods and modpacks", []string{"add", "match", "search", "remove", "list", "update", "outdated", "pin", "unpin", "ignore", "unignore", "suggests", "mod", "modpack", "resourcepack", "shader", "datapack"}},
	{"builds", "Builds", []string{"install", "build", "diff", "pull", "feature", "history", "rollback"}},
	{"launchers", "Launchers", []string{"link", "instances", "instance", "sync", "unlink", "saves", "backup", "restore", "hook"}},
	{"servers", "Servers", []string{"serve", "player"}},
	{"play", "Play", []string{"accounts", "play", "watch"}},
	{"shulker", "Shulker", []string{"docs", "config", "cache", "log", "self", "version", "completion", "help"}},
}

// helpGroupOf is the group a root command is filed in. Hidden commands have one too, so their runs
// are logged under it, though help never shows them.
func helpGroupOf(name string) string {
	for _, g := range helpGroups {
		if slices.Contains(g.Commands, name) {
			return g.ID
		}
	}
	return ""
}

// installHelp files the root's commands into groups and renders every --help
// through the theme instead of cobra's template.
func (a *app) installHelp(root *cobra.Command) {
	for _, g := range helpGroups {
		root.AddGroup(&cobra.Group{ID: g.ID, Title: g.Title})
	}
	help := helpCommand()
	root.SetHelpCommand(help)
	root.AddCommand(help)
	for _, c := range root.Commands() {
		c.GroupID = helpGroupOf(c.Name())
	}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) { a.help(cmd) })
	root.SetUsageFunc(func(cmd *cobra.Command) error { a.help(cmd); return nil })
}

// helpCommand replaces cobra's, which prints a plain "Unknown help topic" and
// exits 0, and ignores words after the last command it recognises.
func helpCommand() *cobra.Command {
	return &cobra.Command{
		Use:         "help [command]...",
		Annotations: reads(),
		Short:       "Show help for a command",
		Hidden:      true,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			parent := cmd.Root()
			for _, word := range args {
				if parent = subcommand(parent, word); parent == nil {
					return nil, cobra.ShellCompDirectiveNoFileComp
				}
			}
			var names []cobra.Completion
			for _, sub := range parent.Commands() {
				if sub.IsAvailableCommand() && strings.HasPrefix(sub.Name(), toComplete) {
					names = append(names, cobra.CompletionWithDesc(sub.Name(), sub.Short))
				}
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			at := cmd.Root()
			for _, word := range args {
				next := subcommand(at, word)
				if next == nil {
					return unknownSubcommand(at, word)
				}
				at = next
			}
			at.InitDefaultHelpFlag()
			return at.Help()
		},
	}
}

func subcommand(parent *cobra.Command, word string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.IsAvailableCommand() && (c.Name() == word || slices.Contains(c.Aliases, word)) {
			return c
		}
	}
	return nil
}

type helpData struct {
	Command     string            `json:"command"`
	Short       string            `json:"short"`
	Description []string          `json:"description"`
	Usage       string            `json:"usage"`
	Commands    []helpDataCommand `json:"commands"`
	Flags       []helpDataFlag    `json:"flags"`
	GlobalFlags []helpDataFlag    `json:"globalFlags"`
	Examples    []string          `json:"examples"`
	Docs        string            `json:"docs"`
}

type helpDataCommand struct {
	Name  string `json:"name"`
	Short string `json:"short"`
}

type helpDataFlag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type,omitempty"`
	Usage     string `json:"usage"`
	Default   string `json:"default,omitempty"`
}

func (a *app) helpJSON(cmd *cobra.Command) {
	path := ""
	if cmd.HasParent() {
		path = strings.TrimPrefix(cmd.CommandPath(), "shulker ")
	}
	if a.printer.Command == "" {
		a.printer.Command = path
	}
	doc, documented := docs.HelpFor(cmd.CommandPath())
	d := helpData{Command: path, Short: cmd.Short, Description: []string{}, Usage: helpUsageText(cmd), Commands: []helpDataCommand{}, Examples: []string{}, Docs: docsURL}
	if documented {
		d.Docs = docsURL + "/cli#" + doc.Anchor
		for _, paragraph := range doc.Description {
			d.Description = append(d.Description, docs.Plain(paragraph))
		}
		d.Examples = append(d.Examples, doc.Examples...)
	} else if cmd.Short != "" {
		d.Description = append(d.Description, cmd.Short)
	}
	if long := strings.TrimSpace(cmd.Long); long != "" {
		d.Description = append(d.Description, strings.Join(strings.Fields(long), " "))
	}
	for _, c := range cmd.Commands() {
		if c.IsAvailableCommand() {
			d.Commands = append(d.Commands, helpDataCommand{Name: c.Name(), Short: c.Short})
		}
	}
	local, global := cmd.NonInheritedFlags(), cmd.InheritedFlags()
	if !cmd.HasParent() {
		local, global = global, local
	}
	d.Flags, d.GlobalFlags = helpDataFlags(local), helpDataFlags(global)
	_ = a.printer.Emit(d, nil)
}

func helpDataFlags(flags *pflag.FlagSet) []helpDataFlag {
	rows := []helpDataFlag{}
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" || f.Hidden {
			return
		}
		kind, usage := flagKind(f)
		rows = append(rows, helpDataFlag{Name: f.Name, Shorthand: f.Shorthand, Type: kind, Usage: usage, Default: flagDefault(f)})
	})
	return rows
}

func (a *app) help(cmd *cobra.Command) {
	if a.printer.JSON {
		a.helpJSON(cmd)
		return
	}
	l := a.printer.Out()
	t := l.T
	doc, documented := docs.HelpFor(cmd.CommandPath())
	description := []string{cmd.Short}
	if documented {
		description = doc.Description
	}
	width := min(out.TerminalWidth(cmd.OutOrStdout()), helpColumns) - len(helpIndent)
	for i, paragraph := range description {
		if i > 0 {
			l.Blank()
		}
		for _, line := range out.Wrap(t.Markup(docs.Plain(paragraph)), width) {
			l.Plain(line)
		}
	}
	l.Blank()
	helpUsageLine(l, cmd)
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
	if len(doc.Examples) > 0 {
		l.Heading("Examples")
		for _, example := range doc.Examples {
			l.Text(helpIndent + t.Grey("$") + " " + t.Command(example))
		}
		l.Blank()
	}
	helpFlags(l, "Global flags", global)
	if cmd.Long != "" {
		for _, line := range strings.Split(strings.TrimRight(cmd.Long, "\n"), "\n") {
			l.Text(t.Grey(line))
		}
		l.Blank()
	}
	if doc.HasMore {
		l.Text(t.Grey("More:"))
		l.Text(helpIndent + t.Grey("$") + " " + t.Command("shulker docs "+strings.TrimPrefix(cmd.CommandPath(), "shulker ")))
		l.Blank()
	}
	url := docsURL
	if documented {
		url = docsURL + "/cli#" + doc.Anchor
	}
	l.Text(t.Grey("Docs") + " " + t.Link(t.Cyan(url), url))
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

func helpUsageLine(l *out.Lines, cmd *cobra.Command) {
	t := l.T
	l.Heading("Usage")
	l.Text(helpIndent + t.Grey("$") + " " + t.Command(helpUsageText(cmd)))
}

func helpUsageText(cmd *cobra.Command) string {
	if cmd.HasAvailableSubCommands() {
		return cmd.CommandPath() + " <command> [flags]"
	}
	return cmd.UseLine()
}

func helpFlags(l *out.Lines, title string, flags *pflag.FlagSet) {
	if rows := flagRows(l.T, flags); len(rows) > 0 {
		printFlagRows(l, title, rows)
		l.Blank()
	}
}

func flagRows(t out.Theme, flags *pflag.FlagSet) []helpFlag {
	var rows []helpFlag
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" || f.Hidden {
			return
		}
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", " + name
		}
		kind, usage := flagKind(f)
		row := helpFlag{head: t.Command(name), usage: usage, width: len(name)}
		if kind != "" {
			row.head += " " + t.Grey("<"+kind+">")
			row.width += len(kind) + 3
		}
		if def := flagDefault(f); def != "" {
			row.aside = t.Aside("default: " + def)
		}
		rows = append(rows, row)
	})
	return rows
}

// flagKind is the flag's value type as help shows it, with a list flag named by its element, and
// its usage without the backquotes that name that type. A flag that takes no value has no type to
// name, so pflag's reading of its first backquoted phrase as one is dropped.
func flagKind(f *pflag.Flag) (kind, usage string) {
	if f.NoOptDefVal != "" {
		return "", f.Usage
	}
	kind, usage = pflag.UnquoteUsage(f)
	return strings.TrimSuffix(strings.TrimSuffix(kind, "Array"), "Slice"), usage
}

func flagDefault(f *pflag.Flag) string {
	switch def := strings.Trim(f.DefValue, "[]"); def {
	case "false", "0":
		return ""
	default:
		return def
	}
}

func printFlagRows(l *out.Lines, title string, rows []helpFlag) {
	width := 0
	for _, r := range rows {
		width = max(width, r.width)
	}
	l.Heading(title)
	for _, r := range rows {
		l.Text(helpIndent + r.head + strings.Repeat(" ", width-r.width+2) + r.usage + r.aside)
	}
}
