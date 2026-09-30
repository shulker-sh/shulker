package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/link"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
)

// linkReport is the link module's report with the sync block as the CLI finishes it: the same JSON
// shape for every launcher, `sync` last.
type linkReport struct {
	link.Report
	Sync *syncResult `json:"sync"`
	rows []out.Row
}

func (r *linkReport) print(l *out.Lines) {
	if r.VersionID != "" {
		l.OKInto("Installed "+r.VersionID, r.VersionDir, "")
	}
	verb := "Created"
	if !r.Created {
		verb = "Updated"
	}
	l.OKInto(verb+" "+r.Noun+" "+r.Shown, r.InstanceDir, "", r.rows...)
	r.Sync.printInto(l)
}

func (a *app) linkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "link",
		Annotations: decides(),
		Short:       "Create a launcher instance that follows a pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return unknownSubcommand(cmd, args[0])
			}
			if !a.canPick() {
				return cmd.Help()
			}
			a.logActing()
			a.warnRawURLs()
			return a.linkAsked(cmd)
		},
	}
	a.scopeFlags(cmd)
	for _, e := range launcher.All {
		cmd.AddCommand(a.launcherLinkCmd(e))
	}
	return cmd
}

// launcherLink is the flags a link takes, the same for every launcher but for the ones its usage
// block leaves out.
type launcherLink struct {
	launcherDir, instanceName, as string
	at                            modpack.At
	force                         bool
	ff                            featureFlags
	ls                            linkSettings
}

func (k *launcherLink) register(cmd *cobra.Command, e *launcher.Entry) {
	if e.HasDir() {
		cmd.Flags().StringVar(&k.launcherDir, "launcher-dir", "", e.Usage.Dir)
	}
	if e.Usage.Names {
		cmd.Flags().StringVar(&k.instanceName, "name", "", e.Usage.Noun+" name (default: the side's display name)")
	}
	as := e.Usage.As
	if as == "" {
		as = "id for this instance, for -i (default: from its name)"
	}
	cmd.Flags().StringVar(&k.as, "as", "", as)
	cmd.Flags().StringVar(&k.at.Ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&k.at.Path, "path", "", "folder of a git source's repository that holds its shulker.json (default: the root)")
	cmd.Flags().BoolVar(&k.force, "force", false, e.Usage.Force)
	k.ff.register(cmd, "for this instance")
	k.ls.register(cmd)
}

// request is the flags as the link module takes them; reason names the command for the history.
func (k *launcherLink) request(reason string) link.Request {
	return link.Request{
		LauncherDir: k.launcherDir,
		Name:        k.instanceName,
		ID:          k.as,
		Force:       k.force,
		With:        k.ff.with,
		Without:     k.ff.without,
		Settings:    k.ls.settings(),
		Reason:      reason,
	}
}

// launcherLinkCmd is `link <launcher>` for one entry: the shared flags and flow around the
// launcher's own placement and link step.
func (a *app) launcherLinkCmd(e *launcher.Entry) *cobra.Command {
	var k launcherLink
	cmd := &cobra.Command{
		Use:         e.Name + " [project-dir | git-url | manifest-url]",
		Annotations: acts(),
		Aliases:     e.Usage.Aliases,
		Short:       e.Usage.Short,
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a.warnRawURLs()
			rep, err := a.linkInto(cmd, args, e, &k)
			if err != nil {
				return err
			}
			return a.printer.Emit(rep, rep.print)
		},
	}
	a.scopeFlags(cmd)
	k.register(cmd, e)
	return cmd
}

// linkInto is the flags, prompts and output of one link: the launcher directory asked for when the
// launcher has no default, the source named or asked for, then link.Into and the rows its report
// prints as.
func (a *app) linkInto(cmd *cobra.Command, args []string, e *launcher.Entry, k *launcherLink) (*linkReport, error) {
	if err := k.ls.check(); err != nil {
		return nil, err
	}
	if e.HasDir() && e.DefaultDir == nil && k.launcherDir == "" {
		dir, err := a.askLauncherDir(e)
		if err != nil {
			return nil, err
		}
		k.launcherDir = dir
	}
	src, err := a.linkFrom(cmd, args, k.at)
	if err != nil {
		return nil, err
	}
	le, err := a.linkEnv()
	if err != nil {
		return nil, err
	}
	rep, err := link.Into(cmd.Context(), le, e, src, k.request(cmd.Name()))
	if err != nil {
		return nil, a.lastOf(err)
	}
	synced, err := a.synced(rep.Sync, syncRequest{}, nil)
	if err != nil {
		return nil, err
	}
	r := &linkReport{Report: *rep, Sync: &synced}
	r.rows = follows(rep.Modpack, rep.Source, rep.Path)
	if rep.Command != "" {
		r.rows = append(r.rows, out.Row{Text: "the launcher syncs this instance before each launch"})
	}
	if rep.FeaturesSaved {
		r.rows = append(r.rows, out.Row{Text: "feature choices saved; change them with `shulker feature on|off <feature> --into " + out.ShellArg(rep.GameDir) + "`"})
	}
	if rep.Note != "" {
		r.rows = append(r.rows, out.Row{Text: rep.Note})
	}
	return r, nil
}

// linkEnv is the link module's env: the sync env, shulker's own instances root, and the launcher
// metadata services a test replaces.
func (a *app) linkEnv() (*link.Env, error) {
	se, err := a.syncEnv()
	if err != nil {
		return nil, err
	}
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	r, err := a.roots()
	if err != nil {
		return nil, err
	}
	return &link.Env{Env: se, Instances: r.Instances, MetaURLs: d.metaURLs}, nil
}

// askLauncherDir asks where a launcher with no default directory is, and off a terminal requires
// the flag.
func (a *app) askLauncherDir(e *launcher.Entry) (string, error) {
	required := out.Errorf("launcher-dir-required", "%s", e.Usage.NoDefault)
	required.Help = "pass --launcher-dir with " + e.Usage.DirHint
	if !a.canPick() {
		return "", required
	}
	dir, err := a.askText("Where is "+e.Title+" installed?", e.Usage.DirHint, "")
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", required
	}
	// A shell expands ~ in --launcher-dir, so the answer to the same question does too.
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, rest)
	}
	return dir, nil
}

// linkAsked is a bare link at a terminal: it asks which launcher, then runs that launcher's own
// link as if it had been named, so everything after the question is the named command's.
func (a *app) linkAsked(cmd *cobra.Command) error {
	var choices []out.Choice
	for _, e := range launcher.All {
		choices = append(choices, out.Choice{Label: e.Title, Value: e.Name})
	}
	name, err := a.ask("Which launcher?", choices)
	if err != nil {
		return err
	}
	sub, _, err := cmd.Find([]string{name})
	if err != nil {
		return err
	}
	sub.SetContext(cmd.Context())
	a.printer.Command = strings.TrimPrefix(sub.CommandPath(), "shulker ")
	return sub.RunE(sub, nil)
}

// linkSource is the project a link command works from: the argument when there
// is one, else the project in the current directory.
func (a *app) linkSource(ctx context.Context, args []string, at modpack.At) (*sync.Source, error) {
	if len(args) == 1 {
		return a.openSource(ctx, args[0], at)
	}
	if at.Ref != "" {
		return nil, out.Errorf("usage", "--ref needs a git source argument")
	}
	if at.Path != "" {
		return nil, out.Errorf("usage", "--path needs a git source argument")
	}
	return a.projectSource()
}

// linkFrom is linkSource for a link command: at a terminal, with nothing to follow, the link
// authors the instance itself.
func (a *app) linkFrom(cmd *cobra.Command, args []string, at modpack.At) (*sync.Source, error) {
	src, err := a.linkSource(cmd.Context(), args, at)
	if len(args) > 0 || !errors.Is(err, project.ErrNoManifest) || !a.canPick() {
		return src, err
	}
	return a.authorSource(cmd)
}

// follows is the row naming what an instance follows, which an authored instance has none of.
func follows(modpack, source, path string) []out.Row {
	if modpack == "" {
		return nil
	}
	if path != "" {
		source += ", path " + path
	}
	return []out.Row{{Text: "follows " + modpack + " from " + source}}
}

// linkSettings are the flags that seed an instance's settings: the hook switches, the marker, and
// the Java and wrapper this machine launches with.
type linkSettings struct {
	noHooks     bool
	noPreLaunch bool
	noPostExit  bool
	noMarker    bool
	withMarker  bool
	java        string
	wrapper     string
}

func (ls *linkSettings) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&ls.noHooks, "no-hooks", false, "install neither hook: don't sync before a launch, don't record how a run ended.")
	cmd.Flags().BoolVar(&ls.noPreLaunch, "no-pre-launch", false, "don't sync this instance before each launch.")
	cmd.Flags().BoolVar(&ls.noPostExit, "no-post-exit", false, "don't record how each run ended.")
	cmd.Flags().BoolVar(&ls.noMarker, "no-marker", false, "leave the marker mod out of this instance's builds.")
	cmd.Flags().BoolVar(&ls.withMarker, "with-marker", false, "include the marker mod in this instance's builds, over a manifest that leaves it out.")
	cmd.Flags().StringVar(&ls.java, "java", "", "absolute path to the Java this machine launches the instance with (default: shulker's managed runtime)")
	cmd.Flags().StringVar(&ls.wrapper, "wrapper", "", "command prefix for the launch command, such as gamemoderun; split on whitespace")
}

func (ls linkSettings) check() error {
	if ls.noMarker && ls.withMarker {
		return out.Errorf("usage", "--no-marker and --with-marker ask for opposite things")
	}
	if ls.java != "" && !filepath.IsAbs(ls.java) {
		return out.Errorf("usage", "--java needs an absolute path: a launcher runs the instance with almost no environment, and nothing searches PATH for it")
	}
	return nil
}

// isSet reports whether this link asks for any setting at all, which is what a mode with no instance
// file to record them in has to refuse.
func (ls linkSettings) isSet() bool {
	return ls.noHooks || ls.noPreLaunch || ls.noPostExit || ls.noMarker || ls.withMarker || ls.java != "" || ls.wrapper != ""
}

// settings is the flags as the link module seeds them. The marker is left to the manifest unless a
// flag decides it.
func (ls linkSettings) settings() link.Settings {
	s := link.Settings{
		NoPreLaunch: ls.noHooks || ls.noPreLaunch,
		NoPostExit:  ls.noHooks || ls.noPostExit,
		Java:        ls.java,
		Wrapper:     strings.Fields(ls.wrapper),
	}
	if ls.noMarker {
		s.Marker = instance.Off()
	}
	if ls.withMarker {
		s.Marker = instance.On()
	}
	return s
}

func (a *app) projectSource() (*sync.Source, error) {
	p, err := a.openProject()
	if err != nil {
		return nil, err
	}
	if err := p.RequireLock(); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return nil, err
	}
	return &sync.Source{Checkout: &modpack.Checkout{Source: dir, Kind: modpack.Local, Dir: dir}, Name: dir, Project: p}, nil
}
