package cli

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
)

type shulkerReport struct {
	Launcher string      `json:"launcher"`
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	GameDir  string      `json:"gameDir"`
	Source   string      `json:"source"`
	Ref      string      `json:"ref,omitempty"`
	Modpack  string      `json:"modpack"`
	Created  bool        `json:"created"`
	Sync     *syncResult `json:"sync"`
}

func (a *app) linkShulkerCmd() *cobra.Command {
	var ref, as string
	var force bool
	var ls linkSettings
	cmd := &cobra.Command{
		Use:   "shulker [project-dir | git-url | manifest-url]",
		Short: "Create an instance shulker owns and launches itself",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, _, err := a.openLinkSource(cmd, args, ref, ls)
			if err != nil {
				return err
			}
			p := src.project
			r, err := a.roots()
			if err != nil {
				return err
			}
			instances, err := a.loadInstances()
			if err != nil {
				return err
			}
			display := p.Manifest.DisplayName("client")
			nick := shulkerNick(instances, r.Instances, as, display)
			gameDir := filepath.Join(r.Instances, nick)
			if err := a.checkID(as, gameDir); err != nil {
				return err
			}
			if err := checkAdopt(gameDir, src, "instance", nick, "--as", force); err != nil {
				return err
			}
			created := false
			if _, err := os.Stat(gameDir); os.IsNotExist(err) {
				created = true
			}
			inst, err := a.linkProject(gameDir, nick, display, ref, src)
			if err != nil {
				return err
			}
			if err := ls.save(gameDir, src.name, ref, "client", false, p.Manifest); err != nil {
				return err
			}
			a.registerInstance(config.Instance{ID: nick, Launcher: "shulker", Name: display, Dir: gameDir, Source: src.name})
			synced, err := a.syncInPlace(cmd, inst, "client", syncRequest{})
			if err != nil {
				return err
			}
			rep := shulkerReport{
				Launcher: "shulker",
				ID:       nick,
				Name:     display,
				GameDir:  gameDir,
				Source:   src.name,
				Ref:      ref,
				Modpack:  modpackKey(inst.Manifest, src.name),
				Created:  created,
				Sync:     &synced,
			}
			return a.printer.Emit(rep, func(l *out.Lines) {
				verb := "created"
				if !created {
					verb = "updated"
				}
				l.OKInto(verb+" instance "+nick, gameDir, "")
				l.Tree(follows(rep.Modpack, rep.Source)...)
				synced.print(l)
			})
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "nickname for this instance, which names its folder and finds it with -i (default: from the pack's name)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint the modpack an instance already follows")
	ls.register(cmd)
	return cmd
}

// shulkerNick is the nickname a shulker instance goes by. It names the folder under the instances
// root as well as the registry id, so the two can never drift and a relink finds its own instance
// by name. Without --as it is a slug of the pack's display name, suffixed until it is either free
// or already the folder it names.
func shulkerNick(instances []config.Instance, root, as, display string) string {
	if as != "" {
		return as
	}
	want := slugID(display)
	for n := 1; ; n++ {
		id := want
		if n > 1 {
			id = want + "-" + strconv.Itoa(n)
		}
		if i, ok := config.FindID(instances, id); !ok || sameDir(instances[i].Dir, filepath.Join(root, id)) {
			return id
		}
	}
}
