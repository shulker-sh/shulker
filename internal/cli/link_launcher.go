package cli

import (
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
)

// launcherLink is what the links into a launcher's own instances share: their flags, the checks
// before the launcher is touched, and the registration and report once the instance is written.
type launcherLink struct {
	launcherDir, instanceName, as string
	at                            pack.At
	force                         bool
	ff                            featureFlags
	ls                            linkSettings
}

type launcherReport struct {
	Launcher    string      `json:"launcher"`
	LauncherDir string      `json:"launcherDir"`
	Instance    string      `json:"instance"`
	InstanceDir string      `json:"instanceDir"`
	Name        string      `json:"name"`
	GameDir     string      `json:"gameDir"`
	Command     string      `json:"command,omitempty"`
	Created     bool        `json:"created"`
	Source      string      `json:"source"`
	Ref         string      `json:"ref,omitempty"`
	Path        string      `json:"path,omitempty"`
	Modpack     string      `json:"modpack"`
	Sync        *syncResult `json:"sync"`
}

func (l *launcherLink) register(cmd *cobra.Command, dirUsage, forceUsage string) {
	cmd.Flags().StringVar(&l.launcherDir, "launcher-dir", "", dirUsage)
	cmd.Flags().StringVar(&l.instanceName, "name", "", "instance name (default: the side's display name)")
	cmd.Flags().StringVar(&l.as, "as", "", "id for this instance, for -i (default: from its name)")
	cmd.Flags().StringVar(&l.at.Ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&l.at.Path, "path", "", "folder of a git source's repository that holds its shulker.json (default: the root)")
	cmd.Flags().BoolVar(&l.force, "force", false, forceUsage)
	l.ff.register(cmd, "for this instance")
	l.ls.register(cmd)
}

func (l *launcherLink) hasFeatures() bool { return len(l.ff.with)+len(l.ff.without) > 0 }

func (l *launcherLink) display(p *project.Project) string {
	if l.instanceName != "" {
		return l.instanceName
	}
	return p.Manifest.DisplayName("client")
}

// openLinkSource is what every link does first: check the settings, fetch the source, refuse a loader
// this build doesn't know, and warn when the pack declares no client.
func (a *app) openLinkSource(cmd *cobra.Command, args []string, at pack.At, ls linkSettings) (*syncSource, loader.Loader, error) {
	if err := ls.check(); err != nil {
		return nil, loader.Loader{}, err
	}
	src, err := a.linkFrom(cmd, args, at)
	if err != nil {
		return nil, loader.Loader{}, err
	}
	p := src.project
	var l loader.Loader
	if p.Lock.Loader.Type != "" {
		if l, err = loader.Require(p.Lock.Loader.Type); err != nil {
			return nil, loader.Loader{}, err
		}
	}
	if !p.Manifest.HasSide("client") {
		a.printer.Warn("%s", noClientPack)
	}
	return src, l, nil
}

// startLauncherLink is openLinkSource plus the feature choices checked against the build, and
// --launcher-dir made absolute, filled from defaultDir when it is empty.
func (a *app) startLauncherLink(cmd *cobra.Command, args []string, k *launcherLink, defaultDir func() (string, error)) (*syncSource, loader.Loader, error) {
	src, l, err := a.openLinkSource(cmd, args, k.at, k.ls)
	if err != nil {
		return nil, l, err
	}
	if k.hasFeatures() {
		b, err := a.builder(cmd.Context(), src.project)
		if err != nil {
			return nil, l, err
		}
		if err := k.ff.check(b); err != nil {
			return nil, l, err
		}
	}
	if k.launcherDir == "" {
		if k.launcherDir, err = defaultDir(); err != nil {
			return nil, l, err
		}
	}
	if k.launcherDir, err = filepath.Abs(k.launcherDir); err != nil {
		return nil, l, err
	}
	return src, l, nil
}

// refuseForeignInstance refuses to link over an instance shulker didn't link, unless --force. An
// instance shulker linked is a project in its own game directory, and stays one after an unlink;
// anything else in that folder is the player's own.
func (a *app) refuseForeignInstance(k *launcherLink, gameDir, title, display string, preLaunch func() (string, bool, error)) error {
	if k.force {
		return nil
	}
	_, _, inPlace, err := a.inPlaceProject(gameDir)
	if err != nil || inPlace {
		return err
	}
	command, found, err := preLaunch()
	if err != nil {
		return err
	}
	if found && !launcher.IsShulkerSlot(command) {
		e := out.Errorf("instance-exists", "%s already has an instance %q that shulker didn't link", title, display)
		e.Help = "pass --name to create a second instance, or --force to link this one"
		return e
	}
	return nil
}

// finishLauncherLink registers an instance the launcher now holds and reports it. extra are the
// launcher's own closing rows.
func (a *app) finishLauncherLink(cmd *cobra.Command, k *launcherLink, launcherName, display string, src *syncSource, res launcher.InstanceResult, extra ...out.Row) error {
	if k.hasFeatures() {
		if err := a.saveInstanceFeatures(res.GameDir, k.ff); err != nil {
			return err
		}
	}
	row := config.Instance{Launcher: launcherName, LauncherDir: k.launcherDir, Name: display, Dir: res.GameDir, Source: src.name}
	inst, synced, err := a.linkInstance(cmd, row, k.as, src, k.ls)
	if err != nil {
		return err
	}
	rep := launcherReport{
		Launcher:    launcherName,
		LauncherDir: k.launcherDir,
		Instance:    filepath.Base(res.Dir),
		InstanceDir: res.Dir,
		Name:        display,
		GameDir:     res.GameDir,
		Command:     launcher.SlotCommand(launcherName, res.GameDir, launcher.HookPreLaunch),
		Created:     res.Created,
		Source:      src.name,
		Ref:         src.Ref,
		Path:        src.Path,
		Modpack:     modpackKey(inst.Manifest, src.name),
		Sync:        &synced,
	}
	return a.printer.Emit(rep, func(l *out.Lines) {
		verb := "created"
		if !res.Created {
			verb = "updated"
		}
		l.OKInto(verb+" instance "+display, res.Dir, "")
		rows := append(follows(rep.Modpack, rep.Source, rep.Path), out.Row{Text: "the launcher syncs this instance before each launch"})
		if k.hasFeatures() {
			rows = append(rows, out.Row{Text: "feature choices saved; change them with `shulker feature on|off <feature> --into " + launcher.CommandArg(res.GameDir) + "`"})
		}
		l.Tree(append(rows, extra...)...)
		synced.print(l)
	})
}
