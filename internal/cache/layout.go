package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"
)

// The cache holds everything shulker can fetch again, shared by every project:
//
//	objects/<aa>/<sha512>          mods, jars, and every other downloaded file
//	tmp/                           partial writes, on the same filesystem so a rename lands
//	logs/installer-<time>.log      the whole output of a loader installer that failed
//	packs/git/<sha>.git            bare mirrors of git pack sources
//	packs/src/<commit>             a commit exported as a tree, what a build reads
//	packs/url/<sha>.json           a manifest fetched from a raw URL
//	projects/url/<sha>             a project source fetched from a URL
//	projects/last-good/<sha>.json  the last sync from a source that built
//	atlauncher/<loader>-<version>/ a loader installer's client install, whose libraries ATLauncher gets
//
// Paths are built here and nowhere else.

func (c *Cache) ATLauncherInstall(loader, version string) string {
	return filepath.Join(c.Dir, "atlauncher", loader+"-"+version)
}

func (c *Cache) InstallerLog(at time.Time) (string, error) {
	dir := filepath.Join(c.Dir, "logs")
	return filepath.Join(dir, "installer-"+at.Format("20060102-150405")+".log"), os.MkdirAll(dir, 0o755)
}

func (c *Cache) Object(sha string) string {
	return filepath.Join(c.Dir, "objects", sha[:2], sha)
}

func (c *Cache) PackMirror(source string) string {
	sum := sha256.Sum256([]byte(source))
	return filepath.Join(c.Dir, "packs", "git", hex.EncodeToString(sum[:8])+".git")
}

func (c *Cache) PackSource(commit string) string {
	return filepath.Join(c.Dir, "packs", "src", commit)
}

func (c *Cache) PackManifest(sha string) string {
	return filepath.Join(c.Dir, "packs", "url", sha+".json")
}

func (c *Cache) ProjectCheckout(sha string) string {
	return filepath.Join(c.Dir, "projects", "url", sha)
}

func (c *Cache) LastGood(source, ref string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + ref))
	return filepath.Join(c.Dir, "projects", "last-good", hex.EncodeToString(sum[:])+".json")
}

func (c *Cache) tempDir() (string, error) {
	dir := filepath.Join(c.Dir, "tmp")
	return dir, os.MkdirAll(dir, 0o755)
}

func (c *Cache) TempFile(prefix string) (*os.File, error) {
	dir, err := c.tempDir()
	if err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, prefix+"-*")
}

func (c *Cache) TempDir(prefix string) (string, error) {
	dir, err := c.tempDir()
	if err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, prefix+"-*")
}
