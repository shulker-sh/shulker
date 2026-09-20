package game

import (
	"fmt"
	"strings"
)

// MojangLibraries is the repository a library naming none of its own comes from, which is what
// the version format has meant by an absent `url` since the format's first release.
const MojangLibraries = "https://libraries.minecraft.net/"

type Library struct {
	Name      string            `json:"name"`
	Downloads *LibraryDownloads `json:"downloads,omitempty"`
	URL       string            `json:"url,omitempty"`
	Natives   map[string]string `json:"natives,omitempty"`
	Extract   *Extract          `json:"extract,omitempty"`
	Rules     []Rule            `json:"rules,omitempty"`
}

type LibraryDownloads struct {
	Artifact    *Artifact           `json:"artifact,omitempty"`
	Classifiers map[string]Artifact `json:"classifiers,omitempty"`
}

type Extract struct {
	Exclude []string `json:"exclude,omitempty"`
}

// IsNative reports whether the library is a jar of platform binaries to unpack beside the
// instance rather than a jar on the classpath. Only the legacy `natives` block makes one: from
// 1.19 on, natives are ordinary rule-gated libraries that stay on the classpath.
func (l Library) IsNative() bool { return len(l.Natives) > 0 }

// Applies reports whether the library belongs in this launch at all: its rules must pass, and a
// native must have a classifier for the platform.
func (l Library) Applies(p Platform, features map[string]bool) bool {
	if !Allows(l.Rules, p, features) {
		return false
	}
	if !l.IsNative() {
		return true
	}
	_, ok := l.nativeClassifier(p)
	return ok
}

// nativeClassifier is the classifier this platform's natives are filed under: the precise
// "<os>-<arch>" key first, then the operating system alone, which only answers where Mojang's
// two-architecture assumption still holds.
func (l Library) nativeClassifier(p Platform) (string, bool) {
	name, ok := l.Natives[p.Classifier()]
	if !ok && p.IsLegacyArch() {
		name, ok = l.Natives[p.OS]
	}
	if !ok {
		return "", false
	}
	bits := "64"
	if p.Arch == "x86" || p.Arch == "arm32" {
		bits = "32"
	}
	return strings.ReplaceAll(name, "${arch}", bits), true
}

// File is the library's jar under the store's libraries directory, taking the native classifier
// for the platform where the library has one. The path is relative to that directory.
func (l Library) File(p Platform) (File, error) {
	if l.IsNative() {
		classifier, ok := l.nativeClassifier(p)
		if !ok {
			return File{}, fmt.Errorf("library %s has no natives for %s", l.Name, p.Classifier())
		}
		if l.Downloads != nil {
			if a, ok := l.Downloads.Classifiers[classifier]; ok {
				return l.fileFrom(a, classifier)
			}
		}
		return l.mavenFile(classifier)
	}
	if l.Downloads != nil && l.Downloads.Artifact != nil {
		return l.fileFrom(*l.Downloads.Artifact, "")
	}
	return l.mavenFile("")
}

func (l Library) fileFrom(a Artifact, classifier string) (File, error) {
	path := a.Path
	if path == "" {
		var err error
		if path, err = mavenPath(l.Name, classifier); err != nil {
			return File{}, err
		}
	}
	return File{Path: path, URL: a.URL, Sha1: a.Sha1, Size: a.Size}, nil
}

func (l Library) mavenFile(classifier string) (File, error) {
	path, err := mavenPath(l.Name, classifier)
	if err != nil {
		return File{}, err
	}
	base := l.URL
	if base == "" {
		base = MojangLibraries
	}
	return File{Path: path, URL: strings.TrimSuffix(base, "/") + "/" + path}, nil
}

// mavenPath is where a library's coordinates put it in a repository:
// group:artifact:version[:classifier][@extension]. A native's classifier replaces the coordinate's
// own, the way the Mojang launcher fills one in.
func mavenPath(coords, classifier string) (string, error) {
	name, ext := coords, "jar"
	if i := strings.LastIndex(coords, "@"); i >= 0 {
		name, ext = coords[:i], coords[i+1:]
	}
	parts := strings.Split(name, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", fmt.Errorf("library %q is not group:artifact:version", coords)
	}
	if classifier == "" && len(parts) == 4 {
		classifier = parts[3]
	}
	group, artifact, version := strings.ReplaceAll(parts[0], ".", "/"), parts[1], parts[2]
	file := artifact + "-" + version
	if classifier != "" {
		file += "-" + classifier
	}
	return group + "/" + artifact + "/" + version + "/" + file + "." + ext, nil
}
