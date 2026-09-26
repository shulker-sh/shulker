// Package link makes a launcher instance follow a source: it places the instance where the
// launcher keeps them, writes the launcher's own files, leaves a project in the game directory,
// seeds its instance file, registers it and builds it. The CLI's link commands and play's create
// prompt are its callers.
package link

import (
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/sync"
)

// Env is what a link reaches beyond a sync: where shulker's own instances live, which stands in
// for the launcher directory of shulker itself, and the launcher metadata services a test replaces.
type Env struct {
	*sync.Env
	Instances string
	// MetaURLs replaces a launcher's metadata service, keyed by the URL its entry names.
	MetaURLs map[string]string
}

// metaURL is where a link reads a launcher's own metadata: the entry's service, unless the run
// points that service at a fake.
func (e *Env) metaURL(entry *launcher.Entry) string {
	if url, ok := e.MetaURLs[entry.MetaURL]; ok {
		return url
	}
	return entry.MetaURL
}
