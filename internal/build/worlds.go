package build

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

// Worlds is where a game directory keeps the worlds it loads. Level is set for a server, which
// loads the one world named by level-name out of Dir and ignores its siblings.
type Worlds struct {
	Dir   string
	Level string
}

// WorldsOf finds the worlds of the game in dir. m is the manifest that builds dir in place, or nil
// when there is none; without it the side comes from the build state, or else from whether dir
// has a server.properties. A separate-dir build's worlds are read where its data link points.
func WorldsOf(dir string, m *manifest.Manifest) (Worlds, error) {
	state, _ := ReadState(dir)
	side := state.Side
	if m != nil {
		side, _ = m.InPlaceSide()
	}
	if side == "" {
		if _, err := os.Stat(filepath.Join(dir, PropertiesFile)); err == nil {
			side = "server"
		}
	}
	if side != "server" {
		return Worlds{Dir: linkTarget(dir, "saves", state)}, nil
	}
	level, err := serverLevelName(dir, m)
	if err != nil {
		return Worlds{}, err
	}
	return Worlds{Dir: filepath.Dir(linkTarget(dir, level, state)), Level: level}, nil
}

func serverLevelName(dir string, m *manifest.Manifest) (string, error) {
	if m != nil {
		var raw map[string]any
		if m.Server != nil {
			raw = m.Server.Properties
		}
		l, lockErr := lock.Load(filepath.Join(dir, lock.FileName))
		props, err := renderProperties(PropertiesFile, raw, templateVars(m, l, "server"))
		// The lock only matters for a value that uses its variables, so a lock that can't be read
		// is only the error once rendering needs it.
		if err != nil && lockErr != nil && !errors.Is(lockErr, fs.ErrNotExist) {
			return "", lockErr
		}
		if err != nil {
			return "", err
		}
		return levelName(props), nil
	}
	data, err := os.ReadFile(filepath.Join(dir, PropertiesFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return levelName(parseProperties(data)), nil
}

func linkTarget(dir, rel string, state State) string {
	abs := filepath.Join(dir, rel)
	if !slices.Contains(state.Links, rel) {
		return abs
	}
	target, err := os.Readlink(abs)
	if err != nil {
		return abs
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Join(dir, target)
}
