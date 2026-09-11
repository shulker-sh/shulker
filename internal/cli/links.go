package cli

import "shulker.sh/shulker/internal/config"

func (a *app) configFile() (string, error) {
	if a.configPath != "" {
		return a.configPath, nil
	}
	return config.Path()
}

func (a *app) registerLink(l config.Link) {
	a.updateLinks(func(links []config.Link) []config.Link {
		if i, ok := config.FindLink(links, l.Dir); ok {
			links[i] = l
			return links
		}
		return append(links, l)
	})
}

// registerSync keeps the launcher of an entry that a link made, and its name unless l has one.
func (a *app) registerSync(l config.Link, defaultName string) (config.Link, bool) {
	changed := a.updateLinks(func(links []config.Link) []config.Link {
		i, ok := config.FindLink(links, l.Dir)
		if !ok {
			if l.Name == "" {
				l.Name = defaultName
			}
			return append(links, l)
		}
		old := links[i]
		l.Launcher, l.LauncherDir = old.Launcher, old.LauncherDir
		if l.Name == "" {
			l.Name = old.Name
		}
		links[i] = l
		return links
	})
	return l, changed
}

func (a *app) updateLinks(update func([]config.Link) []config.Link) bool {
	path, err := a.configFile()
	if err == nil {
		var changed bool
		if changed, err = config.UpdateLinks(path, update); err == nil {
			return changed
		}
	}
	a.progress("warning: config.json not updated: %v", err)
	return false
}
