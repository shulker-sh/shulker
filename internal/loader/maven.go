package loader

import "strings"

// MavenPath is where a group:artifact:version[:classifier][@extension] coordinate's file sits under a
// Maven root; without an extension it is a jar.
func MavenPath(name string) (string, error) {
	coords, ext, ok := strings.Cut(name, "@")
	if !ok {
		ext = "jar"
	}
	parts := strings.Split(coords, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", invalid("the Maven coordinate %q is not group:artifact:version[:classifier]", name)
	}
	group, artifact, version := strings.ReplaceAll(parts[0], ".", "/"), parts[1], parts[2]
	file := artifact + "-" + version
	if len(parts) == 4 {
		file += "-" + parts[3]
	}
	return group + "/" + artifact + "/" + version + "/" + file + "." + ext, nil
}
