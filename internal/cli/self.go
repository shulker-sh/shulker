package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/selfupdate"
)

type selfUpdateResult struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	// Available is nil for a build with no version to compare, a dev build, since "false" would
	// read as up to date.
	Available  *bool  `json:"available"`
	Install    string `json:"install,omitempty"`
	Updated    bool   `json:"updated"`
	Path       string `json:"path,omitempty"`
	Provenance string `json:"provenance,omitempty"`
}

func (a *app) selfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self",
		Short: "Manage the shulker binary itself",
	}
	cmd.AddCommand(a.selfUpdateCmd(), a.selfUninstallCmd())
	return cmd
}

type selfUninstallResult struct {
	Unhooked []config.Instance `json:"unhooked"`
	// Removed is "" when a package manager owns the binary and the uninstall stops short of it.
	Removed   string            `json:"removed"`
	Install   string            `json:"install,omitempty"`
	Renamed   string            `json:"renamed,omitempty"`
	Purged    bool              `json:"purged"`
	Forgotten []config.Instance `json:"forgotten,omitempty"`
}

func (a *app) selfUninstallCmd() *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:         "uninstall",
		Annotations: acts(),
		Short:       "Take shulker out of every launcher it hooked, then remove the binary",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.selfUninstall(purge)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also forget the registry, the index instances repair rebuilds from.")
	return cmd
}

// selfUninstall leaves the instance folders and, unless --purge, the registry, so reinstalling and
// running `instances repair` puts every hook back. Nothing prompts: typing the command is the intent,
// and --purge is a second explicit act. A failure to unhook one instance is a warning, because
// leaving shulker installed over one unreadable launcher file helps nobody. A binary a package
// manager installed is left for it to remove, since that manager tracks the file and this
// command has already done everything else.
func (a *app) selfUninstall(purge bool) error {
	exe, err := a.exe()
	if err != nil {
		return out.Errorf("self-uninstall", "can't find the running shulker binary").WithCause("os", err)
	}
	route := a.build().Route
	res := selfUninstallResult{Unhooked: []config.Instance{}, Install: string(route), Purged: purge}
	instances, err := a.loadInstances()
	if err != nil {
		a.printer.Warn("no instance was unhooked: %v.", err)
	}
	for _, in := range instances {
		e := launcher.Find(in.Launcher)
		if e == nil {
			// A plain `sync --into` directory has no launcher holding a hook of shulker's.
			res.Forgotten = append(res.Forgotten, in)
			continue
		}
		if !e.HasHooks() {
			continue
		}
		// The slot-clearing path unlink uses: an adopted command goes back, shulker's own slots and
		// scripts go, and a Mojang profile gets its own Java back. The row's launcherDir is what
		// reaches the profile of an instance whose folder is gone.
		if _, _, err := launcher.ReleaseSlots(e, in); err != nil {
			a.printer.Warn("%s keeps shulker's hooks: %v.", launcher.Named(in), err)
			continue
		}
		res.Unhooked = append(res.Unhooked, in)
	}
	if route.UninstallCommand() == "" {
		res.Removed = exe
		if runtime.GOOS == "windows" {
			// os.Remove can't touch a running exe, and the .old sweep `self update` relies on happens on
			// the next run, which an uninstall never has.
			res.Renamed = exe + ".old"
			err = os.Rename(exe, res.Renamed)
		} else {
			err = os.Remove(exe)
		}
		if err != nil {
			e := out.Errorf("self-uninstall", "can't remove %s", exe).WithCause("os", err)
			e.Data = res
			return e
		}
	}
	if purge {
		if err := a.forgetRegistry(); err != nil {
			a.printer.Warn("the registry is still there: %v.", err)
			res.Purged, res.Forgotten = false, nil
		}
	} else {
		res.Forgotten = nil
	}
	return a.printer.Emit(res, res.print)
}

func (a *app) forgetRegistry() error {
	path, err := a.registryFile()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s selfUninstallResult) print(l *out.Lines) {
	if len(s.Unhooked) > 0 {
		l.OK("Unhooked "+out.Count(len(s.Unhooked), "instance", "instances"), "")
		items := make([]out.Item, 0, len(s.Unhooked))
		for _, in := range s.Unhooked {
			items = append(items, out.Item{Kind: out.Note, Name: in.ID, Aside: []string{launcher.InstanceAside(in)}})
		}
		l.Items(items...)
	}
	route := selfupdate.Route(s.Install)
	if s.Removed != "" {
		l.OK("Removed "+s.Removed, "")
	} else {
		l.Info("The binary is " + route.Owner() + "'s to remove.")
	}
	if s.Purged {
		l.OK("Forgot the registry", "")
		if len(s.Forgotten) > 0 {
			l.Info(out.Count(len(s.Forgotten), "directory", "directories") + " shulker synced can't be found again by `instances repair`.")
			items := make([]out.Item, 0, len(s.Forgotten))
			for _, in := range s.Forgotten {
				items = append(items, out.Item{Kind: out.Note, Name: in.ID, Aside: []string{in.Dir}})
			}
			l.Items(items...)
		}
	} else {
		l.Info("The registry and every instance folder are untouched.")
		l.Nudge("Reinstall, then", "shulker instances repair")
	}
	if s.Renamed != "" {
		l.Nudge("Delete the leftover binary", `del "`+s.Renamed+`"`)
	}
	if s.Removed == "" {
		l.Nudge("Remove it with", route.UninstallCommand())
	}
}

func (a *app) selfUpdateCmd() *cobra.Command {
	var check, without, require bool
	cmd := &cobra.Command{
		Use:         "update",
		Annotations: decides(),
		Short:       "Update shulker to the latest release",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if without && require {
				return out.Errorf("usage", "--without-attestation and --require-attestation can't be used together")
			}
			if !check {
				a.logActing()
			}
			return a.selfUpdate(cmd.Context(), check, without, require)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only report whether a newer release is available.")
	cmd.Flags().BoolVar(&without, "without-attestation", false, "skip the build provenance check.")
	cmd.Flags().BoolVar(&require, "require-attestation", false, "fail unless gh verifies the build provenance.")
	return cmd
}

func (a *app) selfUpdate(ctx context.Context, check, without, require bool) error {
	r := a.releases
	if r == nil {
		f := fetch.New(a.build().Version)
		f.Waiting = a.printer.Waiting
		r = selfupdate.New(f)
	}
	tag, err := r.Latest(ctx)
	switch {
	case errors.Is(err, fetch.ErrNotFound) && check:
		b := a.build()
		res := selfUpdateResult{Current: b.Version, Install: string(b.Route)}
		return a.printer.Emit(res, func(l *out.Lines) { l.Info("No release published yet") })
	case errors.Is(err, fetch.ErrNotFound):
		return out.Errorf("self-update-check", "no shulker release has been published yet")
	case fetch.IsNetwork(err):
		return out.Errorf("self-update-check", "can't reach GitHub to check for updates").WithCause("github", err)
	case err != nil:
		return out.Errorf("self-update-check", "can't check for updates").WithCause("github", err)
	}
	b := a.build()
	res := selfUpdateResult{Current: b.Version, Latest: strings.TrimPrefix(tag, "v"), Install: string(b.Route)}
	if b.Version != selfupdate.Dev {
		available := selfupdate.NeedsUpdate(b.Version, tag)
		res.Available = &available
	}
	if res.Available != nil && !*res.Available {
		return a.printer.Emit(res, func(l *out.Lines) { l.OK("Shulker is up to date", b.Version) })
	}
	if check {
		return a.printer.Emit(res, func(l *out.Lines) {
			if res.Available == nil {
				l.Info("The latest release is shulker " + res.Latest + ".")
			} else {
				l.Items(out.Item{Kind: out.Change, Name: "shulker", From: b.Version, To: res.Latest, Aside: []string{"update available"}})
			}
			l.Nudge(b.Route.UpdateLead(), b.Route.UpdateCommand())
		})
	}
	if !b.Route.Managed() {
		e := out.Errorf("self-update-unmanaged", "this shulker was %s", b.Origin())
		e.Nudge = out.Nudge{Lead: b.Route.UpdateLead(), Command: b.Route.UpdateCommand()}
		e.Data = res
		return e
	}

	exe, err := a.exe()
	if err != nil {
		return out.Errorf("self-update-install", "can't find the running shulker binary").WithCause("os", err)
	}
	tmp, err := os.MkdirTemp("", "shulker-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	a.progress("downloading shulker %s", res.Latest)
	archive, err := r.Download(ctx, tag, tmp)
	if err != nil {
		var sum *selfupdate.ChecksumError
		if errors.As(err, &sum) {
			e := out.Errorf("self-update-checksum", "%s doesn't match its checksum", sum.Asset)
			e.Rows = []out.Detail{{Label: "want", Text: sum.Want}, {Label: "got", Text: sum.Got}}
			return e
		}
		return out.Errorf("self-update-download", "can't download shulker %s", res.Latest).WithCause("download", err)
	}
	a.progress("checksum verified")
	if res.Provenance, err = a.checkProvenance(ctx, r, tag, archive, without, require); err != nil {
		return err
	}
	if err := selfupdate.Install(archive, exe); err != nil {
		return out.Errorf("self-update-install", "can't replace %s", exe).WithCause("os", err)
	}
	res.Updated, res.Path = true, exe
	if _, err := a.repairInstances("", ""); err != nil {
		a.printer.Warn("instances not repaired: %v", err)
	}
	return a.printer.Emit(res, func(l *out.Lines) {
		l.OKInto("Updated shulker "+l.T.Bump(b.Version, res.Latest), exe, "")
	})
}

func (a *app) checkProvenance(ctx context.Context, r *selfupdate.Releases, tag, archive string, without, require bool) (string, error) {
	if without {
		a.progress("skipping build provenance check (--without-attestation)")
		return "skipped", nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		if require {
			return "", out.Errorf("self-update-provenance", "--require-attestation is set but gh is not installed")
		}
		a.printer.Warn("GitHub CLI `gh` not found, skipping build provenance check")
		return "skipped", nil
	}
	a.progress("verifying build provenance with gh")
	if err := r.VerifyProvenance(ctx, tag, archive); err != nil {
		if require {
			return "", out.Errorf("self-update-provenance", "build provenance could not be verified").WithCause("gh", err)
		}
		a.printer.Warn("build provenance could not be verified, continuing on the checksum.")
		return "unverified", nil
	}
	a.progress("build provenance verified")
	return "verified", nil
}
