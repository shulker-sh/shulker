package cli

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
)

type shulkerReport struct {
	Launcher string      `json:"launcher"`
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	GameDir  string      `json:"gameDir"`
	Source   string      `json:"source"`
	Ref      string      `json:"ref,omitempty"`
	Path     string      `json:"path,omitempty"`
	Modpack  string      `json:"modpack"`
	Created  bool        `json:"created"`
	Sync     *syncResult `json:"sync"`
}

func (a *app) linkShulkerCmd() *cobra.Command {
	var as string
	var at pack.At
	var force bool
	var ls linkSettings
	cmd := &cobra.Command{
		Use:         "shulker [project-dir | git-url | manifest-url]",
		Annotations: acts(),
		Short:       "Create an instance shulker owns and launches itself",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := a.linkShulker(cmd, args, at, as, force, ls)
			if err != nil {
				return err
			}
			return a.printer.Emit(rep, rep.print)
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "nickname for this instance, which names its folder and finds it with -i (default: from the pack's name)")
	cmd.Flags().StringVar(&at.Ref, "ref", "", "branch, tag, or commit to follow from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&at.Path, "path", "", "folder of a git source's repository that holds its shulker.json (default: the root)")
	cmd.Flags().BoolVar(&force, "force", false, "repoint the modpack an instance already follows")
	ls.register(cmd)
	return cmd
}

// linkShulker creates or updates the instance shulker owns for a project, registers it and syncs it.
func (a *app) linkShulker(cmd *cobra.Command, args []string, at pack.At, as string, force bool, ls linkSettings) (shulkerReport, error) {
	src, _, err := a.openLinkSource(cmd, args, at, ls)
	if err != nil {
		return shulkerReport{}, err
	}
	p := src.project
	r, err := a.roots()
	if err != nil {
		return shulkerReport{}, err
	}
	instances, err := a.loadInstances()
	if err != nil {
		return shulkerReport{}, err
	}
	display := p.Manifest.DisplayName("client")
	nick := shulkerNick(instances, r.Instances, as, display)
	gameDir := filepath.Join(r.Instances, nick)
	if err := a.checkID(as, gameDir); err != nil {
		return shulkerReport{}, err
	}
	if err := checkAdopt(gameDir, src, "instance", nick, "--as", force); err != nil {
		return shulkerReport{}, err
	}
	created := false
	if _, err := os.Stat(gameDir); os.IsNotExist(err) {
		created = true
	}
	inst, linked, err := a.linkProject(gameDir, nick, display, src)
	if err != nil {
		return shulkerReport{}, err
	}
	if err := ls.save(gameDir, src.name, src.At, "client", false, p.Manifest); err != nil {
		return shulkerReport{}, err
	}
	a.registerInstance(config.Instance{ID: nick, Launcher: "shulker", Name: display, Dir: gameDir, Source: src.name})
	synced, err := a.syncInPlace(cmd, inst, "client", syncRequest{linked: linked})
	if err != nil {
		return shulkerReport{}, err
	}
	return shulkerReport{
		Launcher: "shulker",
		ID:       nick,
		Name:     display,
		GameDir:  gameDir,
		Source:   src.name,
		Ref:      src.Ref,
		Path:     src.Path,
		Modpack:  modpackKey(inst.Manifest, src.name),
		Created:  created,
		Sync:     &synced,
	}, nil
}

func (r shulkerReport) print(l *out.Lines) {
	verb := "created"
	if !r.Created {
		verb = "updated"
	}
	l.OKInto(verb+" instance "+r.ID, r.GameDir, "")
	l.Tree(follows(r.Modpack, r.Source, r.Path)...)
	r.Sync.print(l)
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
		if i, ok := config.FindID(instances, id); !ok || isSameDir(instances[i].Dir, filepath.Join(root, id)) {
			return id
		}
	}
}
