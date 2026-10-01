package loader

import (
	"net/url"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/security"
)

// lockedElsewhere refuses a loader file the lock downloads from anywhere but where the loader
// publishes it. The lock's author picked the address and the hash, and shulker runs an installer
// jar itself, outside the game.
func lockedElsewhere(l Loader, what, got string) error {
	e := out.Errorf("provenance-mismatch", "the lock has %s downloading from %s, which isn't where %s publishes it", what, got, l.Name)
	e.Help = "remove loader.client and loader.server from the lock that names it, so shulker locks them again"
	return security.Refusal(security.Provenance, e)
}

// checkInstallerURL holds a locked installer jar to the address the loader publishes it at.
func (l Loader) checkInstallerURL(r *Remote, lk *lock.Lock, what, got string) error {
	if got != l.installerURL(r, lk.Minecraft, lk.Loader.Version) {
		return lockedElsewhere(l, what, got)
	}
	return nil
}

// checkInstallerLibraries holds each locked library to the installer's own list: the same name at
// the same address, and, once it is cached, the bytes the installer's sha1 names.
func checkInstallerLibraries(l Loader, c *cache.Cache, s *lock.ServerJar) error {
	listed, err := installerLibraries(c.Object(s.Sha512))
	if err != nil {
		return err
	}
	for name, dl := range s.Libraries {
		i := slices.IndexFunc(listed, func(lib installerLibrary) bool { return lib.name == name })
		if i < 0 || listed[i].url != dl.URL {
			return lockedElsewhere(l, "the library "+name, dl.URL)
		}
		if sum1 := listed[i].sha1; sum1 != "" && c.Has(dl.Sha512) {
			if sha, ok := c.BySha1(sum1); !ok || sha != dl.Sha512 {
				e := out.Errorf("checksum-mismatch", "the lock's library %s isn't the file the %s installer names", name, l.Name)
				e.Help = "remove loader.server from the lock that names it, so shulker locks it again"
				return security.Refusal(security.Provenance, e)
			}
		}
	}
	return nil
}

// checkMavenLibraries holds each locked library to its own Maven path on one of the hosts the
// bases name.
func checkMavenLibraries(l Loader, s *lock.ServerJar, bases ...string) error {
	hosts := make([]string, len(bases))
	for i, base := range bases {
		if u, err := url.Parse(base); err == nil {
			hosts[i] = u.Host
		}
	}
	for name, dl := range s.Libraries {
		path, err := MavenPath(name)
		if err != nil {
			return err
		}
		u, err := url.Parse(dl.URL)
		if err != nil || !slices.Contains(hosts, u.Host) || !strings.HasSuffix(u.Path, "/"+path) {
			return lockedElsewhere(l, "the library "+name, dl.URL)
		}
	}
	return nil
}
