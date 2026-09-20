package cli

import (
	"path/filepath"

	"shulker.sh/shulker/internal/config"
)

// rootDirs are the three directories the config keys of the same names move: where the instances
// shulker owns live, where their save groups live, and where a launch assembles its shared game
// files from. Instances and saves are data a player would miss, so they default under the data
// directory; the store holds only what shulker can fetch again, so it defaults into the cache.
type rootDirs struct {
	Instances string
	Saves     string
	Store     string
}

func (a *app) roots() (rootDirs, error) {
	path, err := a.configFile()
	if err != nil {
		return rootDirs{}, err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return rootDirs{}, err
	}
	return a.rootsOf(path, cfg)
}

func (a *app) rootsOf(configPath string, cfg config.Config) (rootDirs, error) {
	data, err := config.DataDir()
	if err != nil {
		return rootDirs{}, err
	}
	d, err := a.deps()
	if err != nil {
		return rootDirs{}, err
	}
	return rootDirs{
		Instances: config.Root(configPath, cfg.Instances, filepath.Join(data, "instances")),
		Saves:     config.Root(configPath, cfg.Saves, filepath.Join(data, "saves")),
		Store:     config.Root(configPath, cfg.Store, d.cache.Game()),
	}, nil
}
