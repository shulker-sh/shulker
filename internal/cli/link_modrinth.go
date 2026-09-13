package cli

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
)

type modrinthReport struct {
	Launcher    string      `json:"launcher"`
	LauncherDir string      `json:"launcherDir"`
	Name        string      `json:"name"`
	Target      string      `json:"target"`
	InstanceDir string      `json:"instanceDir"`
	Pack        string      `json:"pack,omitempty"`
	Created     bool        `json:"created"`
	Source      string      `json:"source"`
	Ref         string      `json:"ref,omitempty"`
	Sync        *syncResult `json:"sync,omitempty"`
}

func (a *app) linkModrinthCmd() *cobra.Command {
	var launcherDir, target, instanceName, ref string
	var force bool
	cmd := &cobra.Command{
		Use:   "modrinth [project-dir | git-url | manifest-url]",
		Short: "Create a Modrinth App instance from the client build",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := a.linkSource(cmd.Context(), args, ref)
			if err != nil {
				return err
			}
			p := src.project
			if _, err := loader.Require(p.Lock.Loader.Type); err != nil {
				return err
			}
			name, err := sideTarget(p.Manifest, target, "client", "link")
			if err != nil {
				return err
			}
			if launcherDir == "" {
				if launcherDir, err = launcher.DefaultModrinthDir(); err != nil {
					return err
				}
			}
			if launcherDir, err = filepath.Abs(launcherDir); err != nil {
				return err
			}
			m := &launcher.Modrinth{Dir: launcherDir, Open: a.openFile, Timeout: a.waitFor}
			if err := m.Check(); errors.Is(err, launcher.ErrNotFound) {
				return out.Errorf("launcher-not-found", "no Modrinth App directory at %s; run the app once or pass --launcher-dir", launcherDir)
			} else if err != nil {
				return err
			}
			display := p.Manifest.DisplayName(name)
			if instanceName != "" {
				display = instanceName
			}
			link := config.Link{Launcher: "modrinth", LauncherDir: launcherDir, Side: "client", Name: display, Source: src.name, Target: name, Ref: ref}
			rep := modrinthReport{Launcher: "modrinth", LauncherDir: launcherDir, Name: display, Target: name, Source: src.name, Ref: ref}
			if prev, ok := a.findLauncherLink("modrinth", launcherDir, display); ok {
				if prev.Source != src.name && !force {
					return out.Errorf("instance-exists", "instance %q already syncs from %s; pass --name to create a second instance, or --force to repoint this one", display, prev.Source)
				}
				link.Dir = prev.Dir
				a.registerLink(link)
				r, err := a.sync(cmd.Context(), src, syncRequest{ref: ref, target: name, into: prev.Dir})
				if err != nil {
					return err
				}
				rep.InstanceDir, rep.Sync = prev.Dir, &r
				return a.printer.Emit(rep, func(l *out.Lines) {
					l.OKInto("updated instance "+display, prev.Dir, "")
					r.print(l)
				})
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			packPath := d.cache.Mrpack(profileKey(display))
			exported, err := b.ExportMrpack(build.MrpackOptions{Targets: []string{name}, Name: display, VersionID: packVersion(src), Output: packPath, Bundle: true, Local: true, OS: build.DetectOS()})
			if err != nil {
				return err
			}
			a.warn(exported.Warnings)
			before, err := m.Instances()
			if err != nil {
				return err
			}
			expected := m.InstanceFolder(display)
			fallback := "shulker sync " + launcher.ShellArg(src.name) + " --target " + name + " --into " + launcher.ShellArg(expected) + " --name " + launcher.ShellArg(display)
			a.progress("Opening %s in Modrinth App", filepath.Base(packPath))
			if err := m.OpenMrpack(packPath); err != nil {
				e := out.Errorf("open-failed", "could not open %s in Modrinth App: %v", packPath, err)
				e.Nudge = out.Nudge{Lead: "Open the file in the app yourself, then adopt the instance it makes", Command: fallback}
				return e
			}
			a.progress("Waiting for Modrinth App to create the instance; confirm there if it asks")
			dir, err := m.WaitForInstance(before, expected)
			if errors.Is(err, launcher.ErrInstanceNotCreated) {
				e := out.Errorf("instance-not-created", "Modrinth App did not create an instance for %q within %s", display, waitText(m.Timeout))
				e.Nudge = out.Nudge{Lead: "If the app is still asking, confirm there, then adopt the instance it makes", Command: fallback}
				return e
			}
			if err != nil {
				return err
			}
			link.Dir = dir
			a.registerLink(link)
			rep.InstanceDir, rep.Pack, rep.Created = dir, packPath, true
			return a.printer.Emit(rep, func(l *out.Lines) {
				l.OKInto("created instance "+display, dir, "")
				l.Tree(
					out.Row{Text: "Modrinth App is installing " + filepath.Base(packPath) + "; play it from the app once that finishes"},
					out.Row{Text: "`shulker sync --instance " + launcher.CommandArg(display) + "` brings it up to date from " + src.name + " later"},
				)
			})
		},
	}
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "Modrinth App data directory (default: the app's own)")
	cmd.Flags().StringVar(&target, "target", "", "client target to link (default: the only client target)")
	cmd.Flags().StringVar(&instanceName, "name", "", "instance name (default: the target's display name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint an instance that syncs from a different source")
	return cmd
}

func packVersion(src *syncSource) string {
	if v := src.project.Manifest.Version; v != "" {
		return v
	}
	if len(src.Commit) >= 12 {
		return src.Commit[:12]
	}
	return time.Now().UTC().Format("2006.01.02")
}

func waitText(d time.Duration) string {
	if d == 0 {
		d = 2 * time.Minute
	}
	return d.Round(time.Second).String()
}
