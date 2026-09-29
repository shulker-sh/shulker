package sync

import (
	"cmp"
	"strings"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/security"
)

// ChangeRows are c one line each: the entries added from a provider, the files no provider
// published, then the entries now locked from another project.
func ChangeRows(c *instance.Changes, ps provider.Providers) []string {
	var rows []string
	for _, a := range c.Added {
		rows = append(rows, "adds "+a.Key+" from "+ps.Title(a.Provider))
	}
	for _, u := range c.Unpublished {
		rows = append(rows, "adds "+cmp.Or(u.Key, u.Path)+", which no provider published")
	}
	for _, m := range c.Moved {
		rows = append(rows, m.Key+" moves from "+ps.Title(m.WasProvider)+" project "+m.WasProject+" to "+ps.Title(m.Provider)+" project "+m.Project)
	}
	return rows
}

func changeCount(c *instance.Changes) int {
	return len(c.Added) + len(c.Unpublished) + len(c.Moved)
}

// ChangesWarning is what a sync that nobody was asked about says of the changes it applied, so the
// launcher's log and the command log keep them.
func ChangesWarning(c *instance.Changes, ps provider.Providers) out.SecurityWarning {
	headline := "This sync brought " + out.Count(changeCount(c), "change", "changes") + ", applied without asking."
	return security.Warn(security.SyncReview, headline+"\n"+strings.Join(ChangeRows(c, ps), "\n"), c)
}
