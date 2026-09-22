package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

// addPickLimit is how many results each provider offers a bare add.
const addPickLimit = 10

// askAdd asks a bare add what to add: a search that follows the query, where several can be
// marked. It returns each marked project's id, and the provider it was found on.
func (a *app) askAdd(cmd *cobra.Command, kind, providerName string) ([]string, map[string]string, error) {
	if _, err := a.deps(); err != nil {
		return nil, nil, err
	}
	names := manifest.DefaultProviders
	if providerName != "" {
		names = []string{providerName}
	}
	a.printer.Settle()
	ctx := cmd.Context()
	s := newLiveSearch(a.printer.ErrTheme, func(query string) (searchReply, error) {
		reply, err := a.search(ctx, query, kind, names, addPickLimit, false)
		reply.results.Results = slices.DeleteFunc(reply.results.Results, func(hit searchHit) bool {
			return !isAddable(hit.Type)
		})
		return reply, err
	})
	marked, err := a.questions().BrowseMarks("Add which "+addNoun(kind)+"?", "type to filter, space to mark, enter to add", s, a.stdin)
	if err != nil {
		return nil, nil, escaped(err)
	}
	if len(marked) == 0 {
		return nil, nil, escaped(out.ErrPickCancelled)
	}
	ids := make([]string, 0, len(marked))
	from := map[string]string{}
	for _, value := range marked {
		name, id, _ := strings.Cut(value, ":")
		ids = append(ids, id)
		from[id] = name
	}
	return ids, from, nil
}

// isAddable is a type a search result can be added as: a modpack is added from its source, and a
// provider lists kinds shulker has no entry for at all.
func isAddable(kind string) bool {
	switch kind {
	case "", manifest.TypeMod, manifest.TypeResourcePack, manifest.TypeShader, manifest.TypeDatapack:
		return true
	}
	return false
}

func addNoun(kind string) string {
	switch kind {
	case manifest.TypeResourcePack:
		return "resource packs"
	case manifest.TypeShader:
		return "shaders"
	case manifest.TypeDatapack:
		return "datapacks"
	}
	return "mods"
}
