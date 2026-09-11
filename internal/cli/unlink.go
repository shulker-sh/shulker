package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

type unlinkResult struct {
	config.Link
	OK       bool       `json:"ok"`
	Removed  string     `json:"removed,omitempty"`
	Relink   string     `json:"relink"`
	RelinkIn string     `json:"relinkIn,omitempty"`
	Error    *out.Error `json:"error,omitempty"`
	summary  string
}

const (
	removedPreLaunch = "pre-launch command"
	removedProfile   = "launcher profile"
)

func (a *app) unlinkCmd() *cobra.Command {
	var sel linkSelection
	cmd := &cobra.Command{
		Use:   "unlink [name | dir]",
		Short: "Stop syncing a linked instance or synced directory and forget it, keeping its files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			if query == "" && !sel.all {
				return out.Errorf("usage", "name the entry to unlink, or pass --all; `shulker links` lists them")
			}
			links, err := a.selectLinks(query, sel)
			if err != nil {
				return err
			}
			path, err := a.configFile()
			if err != nil {
				return err
			}
			results := []unlinkResult{}
			failed := 0
			for _, l := range links {
				r, err := a.unlink(path, l)
				if err != nil {
					failed++
					r.OK, r.Error = false, out.AsError(err)
				}
				results = append(results, r)
			}
			if failed > 0 && len(links) == 1 {
				return results[0].Error
			}
			printResults := func(w io.Writer) {
				for i, r := range results {
					if i > 0 {
						fmt.Fprintln(w)
					}
					if !r.OK {
						fmt.Fprintf(w, "couldn't unlink %q: %s\n", r.Name, r.Error.Message)
						continue
					}
					fmt.Fprintln(w, r.summary)
					if r.RelinkIn != "" {
						fmt.Fprintf(w, "to link it again, in %s: %s\n", r.RelinkIn, r.Relink)
					} else if r.Launcher != "" {
						fmt.Fprintf(w, "to link it again: %s\n", r.Relink)
					} else {
						fmt.Fprintf(w, "to register it again: %s\n", r.Relink)
					}
				}
			}
			if failed > 0 {
				if !a.printer.JSON {
					printResults(a.printer.Stdout)
				}
				e := out.Errorf("unlink-failed", "%d of %d entries couldn't be unlinked", failed, len(links))
				e.Data = results
				return e
			}
			return a.printer.Emit(results, printResults)
		},
	}
	sel.register(cmd, "unlink every entry the name matches, or every entry when there's no name")
	return cmd
}

func (a *app) unlink(configPath string, l config.Link) (unlinkResult, error) {
	r := unlinkResult{Link: l, OK: true}
	r.Relink, r.RelinkIn = relinkCommand(l)
	title := launcherTitle(l.Launcher)
	switch l.Launcher {
	case "prism", "multimc":
		instanceDir := filepath.Dir(l.Dir)
		_, statErr := os.Stat(instanceDir)
		switch info, err := os.Lstat(l.Dir); {
		case errors.Is(statErr, os.ErrNotExist):
			r.summary = fmt.Sprintf("unlinked %q (%s); its instance was already gone", l.Name, title)
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			r.summary = fmt.Sprintf("unlinked %q (%s); the instance stays and still uses the build directory", l.Name, title)
		default:
			removed, err := launcher.RemovePreLaunch(instanceDir, l.Launcher == "multimc")
			if err != nil {
				return r, err
			}
			if removed {
				r.Removed = removedPreLaunch
				r.summary = fmt.Sprintf("unlinked %q (%s): removed its pre-launch sync; the instance and its worlds stay\nRestart the launcher if it is open so the change is picked up.", l.Name, title)
			} else {
				r.summary = fmt.Sprintf("unlinked %q (%s); its pre-launch command isn't a shulker sync, so it was kept", l.Name, title)
			}
		}
	case "mojang":
		n, err := (&launcher.Mojang{Dir: l.LauncherDir}).RemoveProfiles(l.Dir)
		if err != nil {
			return r, err
		}
		r.summary = fmt.Sprintf("unlinked %q (%s); it had no launcher profile left", l.Name, title)
		if n > 0 {
			r.Removed = removedProfile
			r.summary = fmt.Sprintf("unlinked %q (%s): removed its launcher profile; the build directory and the loader stay", l.Name, title)
		}
	default:
		r.summary = fmt.Sprintf("forgot %q (%s); its files stay", l.Name, l.Dir)
	}
	_, err := config.UpdateLinks(configPath, func(links []config.Link) []config.Link {
		if i, ok := config.FindLink(links, l.Dir); ok {
			return append(links[:i], links[i+1:]...)
		}
		return links
	})
	return r, err
}

func relinkCommand(l config.Link) (command, in string) {
	args := []string{"shulker"}
	switch l.Launcher {
	case "prism", "multimc":
		args = append(args, "link", l.Launcher)
		if info, err := os.Lstat(l.Dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
			in = l.Source
			args = append(args, "--mode", "symlink")
		} else {
			args = append(args, shellArg(l.Source))
			if l.Ref != "" {
				args = append(args, "--ref", shellArg(l.Ref))
			}
		}
		args = append(args, "--target", shellArg(l.Target), "--name", shellArg(l.Name))
	case "mojang":
		in = l.Source
		args = append(args, "link", "mojang", "--target", shellArg(l.Target))
	default:
		args = append(args, "sync", shellArg(l.Source))
		if l.Ref != "" {
			args = append(args, "--ref", shellArg(l.Ref))
		}
		return strings.Join(append(args, "--target", shellArg(l.Target), "--into", shellArg(l.Dir), "--name", shellArg(l.Name)), " "), ""
	}
	if l.LauncherDir != "" && l.LauncherDir != defaultLauncherDir(l.Launcher) {
		args = append(args, "--launcher-dir", shellArg(l.LauncherDir))
	}
	return strings.Join(args, " "), in
}

func defaultLauncherDir(name string) string {
	var dir string
	switch name {
	case "prism":
		dir, _ = launcher.DefaultPrismDir()
	case "mojang":
		dir, _ = launcher.DefaultMojangDir()
	}
	return dir
}

var plainShellArg = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

func shellArg(s string) string {
	if plainShellArg.MatchString(s) {
		return s
	}
	return launcher.CommandArg(s)
}
