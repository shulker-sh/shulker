package loader

import (
	"regexp"
	"strings"
)

// MavenCoordinate is the grammar of a coordinate MavenPath takes, which the lock's schema holds
// loader.server.libraries keys to as well. No part starts with a dot or holds a slash, so the
// path never has a `..` segment and stays under the root it is joined onto.
const MavenCoordinate = `^[A-Za-z0-9_+-][A-Za-z0-9._+-]*(:[A-Za-z0-9_+-][A-Za-z0-9._+-]*){2,3}(@[A-Za-z0-9]+)?$`

var mavenCoordinate = regexp.MustCompile(MavenCoordinate)

// MavenPath is where a group:artifact:version[:classifier][@extension] coordinate's file sits under a
// Maven root; without an extension it is a jar.
func MavenPath(name string) (string, error) {
	if !mavenCoordinate.MatchString(name) {
		return "", invalid("the Maven coordinate %q is not group:artifact:version[:classifier]", name)
	}
	coords, ext, ok := strings.Cut(name, "@")
	if !ok {
		ext = "jar"
	}
	parts := strings.Split(coords, ":")
	group, artifact, version := strings.ReplaceAll(parts[0], ".", "/"), parts[1], parts[2]
	file := artifact + "-" + version
	if len(parts) == 4 {
		file += "-" + parts[3]
	}
	return group + "/" + artifact + "/" + version + "/" + file + "." + ext, nil
}
