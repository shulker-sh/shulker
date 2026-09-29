package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func (a *app) listCmd() *cobra.Command { return a.listCmdFor("") }

func (a *app) listCmdFor(kind string) *cobra.Command {
	var typ string
	cmd := &cobra.Command{
		Use:         "list",
		Annotations: reads(),
		Aliases:     []string{"ls"},
		Short:       listShort(kind),
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			chosen, err := chooseType(cmd, kind, typ, "")
			if err != nil {
				return err
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			var status project.ModpackStatus
			if chosen == "" || chosen == manifest.TypeModpack {
				store, err := a.packStore(p)
				if err != nil {
					return err
				}
				status = store.Status
			}
			res, err := project.ListEntries(p, chosen, status)
			if err != nil {
				return err
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				if len(res) == 0 {
					printEmptyList(l, chosen)
					return
				}
				printList(l, res, p.Manifest.Sides())
			})
		},
	}
	a.scopeFlags(cmd)
	if kind == "" {
		cmd.Flags().StringVar(&typ, "type", "", typeFlagUsage)
	}
	return cmd
}

func listShort(kind string) string {
	switch kind {
	case manifest.TypeModpack:
		return "List modpacks with their locked ref and whether a local modpack has changed"
	case "":
		return "List everything in requires with its locked version"
	}
	return "List the project's " + kind + "s with their locked versions"
}

// printEmptyList is the empty state for kind, with the command that adds the first of it.
func printEmptyList(l *out.Lines, kind string) {
	switch kind {
	case manifest.TypeModpack:
		l.Info("No modpacks yet")
		l.Nudge("Add a modpack", "shulker modpack add <source>")
	case manifest.TypeResourcePack:
		l.Info("No resource packs yet")
		l.Nudge("Add a resource pack", "shulker resourcepack add <pack>")
	case manifest.TypeShader:
		l.Info("No shaders yet")
		l.Nudge("Add a shader", "shulker shader add <shader>")
	case manifest.TypeDatapack:
		l.Info("No datapacks yet")
		l.Nudge("Add a datapack", "shulker datapack add <datapack>")
	default:
		l.Info("No mods yet")
		l.Nudge("Add a mod", "shulker add <mod>")
	}
}

func printList(l *out.Lines, res []project.ListEntry, sides []string) {
	blocks := []struct{ heading, kind string }{
		{"Modpacks", manifest.TypeModpack},
		{"Mods", manifest.TypeMod},
		{"Resource packs", manifest.TypeResourcePack},
		{"Shaders", manifest.TypeShader},
	}
	printed := false
	for _, b := range blocks {
		var items []out.Item
		for _, e := range res {
			if e.Type == b.kind {
				items = append(items, listItem(l, e, sides))
			}
		}
		if len(items) == 0 {
			continue
		}
		if printed {
			l.Blank()
		}
		printed = true
		l.Heading(b.heading)
		l.Items(items...)
	}
}

func listItem(l *out.Lines, e project.ListEntry, sides []string) out.Item {
	if e.Kind == modpack.Hosted {
		it := out.Item{Kind: out.Note, Name: e.Key, Version: e.Version, Aside: []string{"modpack"}}
		if e.Version == "" {
			it.Aside = append(it.Aside, "not locked")
		}
		if e.Channel != "" && e.Channel != "release" {
			it.Aside = append(it.Aside, "channel: "+e.Channel)
		}
		if e.Pinned {
			it.Aside = append(it.Aside, "pinned")
		}
		return it
	}
	if e.Type == manifest.TypeModpack {
		aside := []string{string(e.Kind)}
		if e.Ref != "" {
			aside = append(aside, "ref "+e.Ref)
		}
		if e.Path != "" {
			aside = append(aside, "path "+e.Path)
		}
		if e.Version != "" {
			pin := e.Version
			if e.Kind == modpack.Git {
				pin = pin[:min(len(pin), 7)]
			}
			aside = append(aside, pin)
		}
		if e.State != "ok" {
			aside = append(aside, e.State)
		}
		return out.Item{Kind: out.Note, Name: e.Key, Text: l.T.Grey(e.Source), Aside: aside}
	}
	it := out.Item{Kind: out.Note, Name: e.Key, Version: e.Version}
	switch {
	case e.File != "":
		it.Text = l.T.Grey(e.File)
		it.Aside = append(it.Aside, "local file")
	case e.Version == "":
		it.Aside = append(it.Aside, "not locked")
	}
	it.Sides, it.OfSides = landsOn(e.Side, sides), len(sides)
	if text := conditionText("os", e.OS); text != "" {
		it.Aside = append(it.Aside, text)
	}
	if text := conditionText("feature", e.Feature); text != "" {
		it.Aside = append(it.Aside, text)
	}
	if e.Channel != "" && e.Channel != "release" {
		it.Aside = append(it.Aside, "channel: "+e.Channel)
	}
	if e.Pinned {
		it.Aside = append(it.Aside, "pinned")
	}
	switch {
	case e.Modpack != "":
		it.Aside = append(it.Aside, "from "+e.Modpack)
	case len(e.RequiredBy) > 0:
		it.Aside = append(it.Aside, "required by "+strings.Join(e.RequiredBy, ", "))
	}
	return it
}

// landsOn is which of the declared sides an entry with side ships on.
func landsOn(side string, declared []string) []string {
	if side == "" || side == "both" {
		return declared
	}
	if slices.Contains(declared, side) {
		return []string{side}
	}
	return nil
}
