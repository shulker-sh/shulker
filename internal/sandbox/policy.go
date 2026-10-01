// Package sandbox confines a game to what its own launch names: it reads a java argv for the
// runtime, the classpath, the natives, the assets and the game directory, and runs Java with the
// operating system refusing everything else under the player's home. Nothing here knows a
// launcher; every launcher hands over the same argv.
package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Policy is what a sandboxed game may touch. Directories cover everything beneath them.
type Policy struct {
	// Read are the directories and ReadFiles the files the game may only read.
	Read      []string
	ReadFiles []string
	// Write are the directories the game may write as well as read.
	Write []string
	// Protect are directories and ProtectFiles files inside Write that stay read-only, and Reopen
	// the directories inside Protect that are writable again.
	Protect      []string
	ProtectFiles []string
	Reopen       []string
}

// Options are what a policy needs that the argv can't say.
type Options struct {
	// SavesRoot is where shulker keeps save groups. A game directory's saves link is followed only
	// into it, since the game can repoint a link in a directory it may write.
	SavesRoot string
	// ProtectFiles are files the game directory holds that the game must not rewrite, such as a
	// launcher's own record of how to start it.
	ProtectFiles []string
}

// Supported reports whether this machine can sandbox a game.
func Supported() bool { return Available() == nil }

// ErrNoGameDir is an argv that names no --gameDir, so there is nothing to confine the game to.
var ErrNoGameDir = errors.New("the java arguments name no --gameDir")

// pathLists are the java options whose next argument is a list of paths the game reads.
var pathLists = []string{"-cp", "-classpath", "--class-path", "-p", "--module-path"}

// writeProperties are the system properties naming a folder the game unpacks native libraries
// into, and readProperties those naming paths it only reads. java.library.path is read: the
// launcher fills it before the game starts, and it can name a system folder.
var (
	writeProperties = []string{"org.lwjgl.system.SharedLibraryExtractPath", "jna.tmpdir", "io.netty.native.workdir"}
	readProperties  = []string{"java.library.path", "libraryDirectory", "legacyClassPath", "log4j.configurationFile"}
)

// Derive reads the policy for a launch off its java argv.
func Derive(java string, argv []string, o Options) (Policy, error) {
	var p Policy
	read := func(paths ...string) {
		for _, path := range paths {
			if path = resolve(path); path == "" {
				continue
			}
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				p.ReadFiles = append(p.ReadFiles, path)
				continue
			}
			p.Read = append(p.Read, path)
		}
	}
	write := func(paths ...string) {
		for _, path := range paths {
			if path = resolve(path); path != "" {
				p.Write = append(p.Write, path)
			}
		}
	}

	// An allow that holds the home folder would undo the sandbox, whatever the argv says.
	home, _ := os.UserHomeDir()
	if home = resolve(home); home == "" {
		home = string(filepath.Separator)
	}
	narrow := func(paths []string) []string {
		return slices.DeleteFunc(paths, func(path string) bool { return within(home, path) })
	}

	runtime := resolve(java)
	if home := filepath.Dir(runtime); filepath.Base(home) == "bin" {
		read(filepath.Dir(home))
	} else {
		read(home)
	}
	gameDir := ""
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		value := ""
		if i+1 < len(argv) {
			value = argv[i+1]
		}
		switch {
		case slices.Contains(pathLists, arg):
			read(filepath.SplitList(value)...)
			i++
		case strings.HasPrefix(arg, "-D"):
			key, value, _ := strings.Cut(arg[2:], "=")
			switch {
			case slices.Contains(writeProperties, key):
				write(filepath.SplitList(value)...)
			case slices.Contains(readProperties, key):
				read(filepath.SplitList(value)...)
			}
		case arg == "--gameDir":
			gameDir = value
			i++
		case strings.HasPrefix(arg, "--gameDir="):
			gameDir = strings.TrimPrefix(arg, "--gameDir=")
		case arg == "--assetsDir":
			read(value)
			i++
		case strings.HasPrefix(arg, "--assetsDir="):
			read(strings.TrimPrefix(arg, "--assetsDir="))
		}
	}
	if gameDir == "" {
		return Policy{}, ErrNoGameDir
	}
	gameDir = resolve(gameDir)
	write(gameDir)
	p.Protect = []string{filepath.Join(gameDir, "mods"), filepath.Join(gameDir, ".shulker")}
	p.Reopen = []string{filepath.Join(gameDir, ".shulker", "logs"), filepath.Join(gameDir, ".shulker", "natives")}
	for _, file := range o.ProtectFiles {
		if file = resolve(file); within(file, gameDir) {
			p.ProtectFiles = append(p.ProtectFiles, file)
		}
	}
	if o.SavesRoot != "" {
		if saves := resolve(filepath.Join(gameDir, "saves")); within(saves, resolve(o.SavesRoot)) {
			write(saves)
		}
	}
	p.Read, p.Write = narrow(p.Read), narrow(p.Write)
	p.Read = slices.DeleteFunc(p.Read, func(path string) bool { return slices.Contains(p.Write, path) })
	if !slices.Contains(p.Write, gameDir) {
		return Policy{}, fmt.Errorf("the game directory %s holds the home folder", gameDir)
	}
	for _, list := range []*[]string{&p.Read, &p.ReadFiles, &p.Write, &p.ProtectFiles} {
		slices.Sort(*list)
		*list = slices.Compact(*list)
	}
	return p, nil
}

// resolve is a path as the operating system will check it: absolute, with every link followed
// as far as the path exists.
func resolve(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	rest := ""
	for dir := abs; ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		if dir == filepath.Dir(dir) {
			return abs
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// within reports whether path is dir or beneath it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
