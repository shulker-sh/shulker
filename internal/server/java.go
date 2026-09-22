package server

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/mcver"
	"shulker.sh/shulker/internal/out"
)

// Java is a java executable and the major version it reports.
type Java struct {
	Path  string `json:"path"`
	Major int    `json:"major"`
}

var versionLine = regexp.MustCompile(`version "([^"]+)"`)

// JavaBin is the java executable under a runtime home.
func JavaBin(home string) string {
	bin := filepath.Join(home, "bin", "java")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	return bin
}

// JavaAt is the java under a Java home.
func JavaAt(home string) (Java, error) {
	bin := JavaBin(home)
	if _, err := exec.LookPath(bin); err != nil {
		return Java{}, out.Errorf("java-not-found", "no java executable under %s (looked for %s)", home, bin)
	}
	major, err := javaMajor(bin)
	if err != nil {
		return Java{}, err
	}
	return Java{Path: bin, Major: major}, nil
}

// ClientJava is the java a `java` setting names: the binary at path, or the one under it when path
// is a Java home. It is refused when it is older than the Minecraft version needs.
func ClientJava(path string, required int) (Java, error) {
	bin := path
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		bin = JavaBin(path)
	}
	if _, err := exec.LookPath(bin); err != nil {
		e := out.Errorf("java-not-found", "no java executable at %s", path)
		e.Help = "the java setting takes the path of a java binary or a Java home"
		return Java{}, e
	}
	major, err := javaMajor(bin)
	if err != nil {
		return Java{}, err
	}
	j := Java{Path: bin, Major: major}
	return j, requireMajor(j, required)
}

// FindJava picks a server's java. An absolute override is a Java home; any other override is a
// version range the java on PATH must fall in, and without one it must reach required.
func FindJava(override string, required int) (Java, error) {
	if filepath.IsAbs(override) {
		j, err := JavaAt(override)
		if err != nil {
			return Java{}, err
		}
		return j, requireMajor(j, required)
	}
	path, err := exec.LookPath("java")
	if err != nil {
		e := out.Errorf("java-not-found", "no java on PATH")
		e.Help = "install a JDK or set \"java\" in shulker.json to a JDK path"
		return Java{}, e
	}
	major, err := javaMajor(path)
	if err != nil {
		return Java{}, err
	}
	j := Java{Path: path, Major: major}
	if override != "" {
		r, err := mcver.ParseRange(override)
		if err != nil {
			return j, out.Errorf("java-range", "manifest java %q is neither an absolute path nor a version range", override)
		}
		if !r.Contains(mcver.MustParse(fmt.Sprintf("%d.0.0", major))) {
			return j, out.Errorf("java-version", "java at %s is version %d, outside the manifest range %q", path, major, override)
		}
		return j, nil
	}
	return j, requireMajor(j, required)
}

func requireMajor(j Java, required int) error {
	if j.Major < required {
		e := out.Errorf("java-version", "java at %s is version %d; this Minecraft version needs Java %d or newer", j.Path, j.Major, required)
		e.Help = "set \"java\" in shulker.json to a JDK path or put a newer java on PATH"
		return e
	}
	return nil
}

func javaMajor(path string) (int, error) {
	var stderr, stdout bytes.Buffer
	cmd := exec.Command(path, "-version")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return 0, out.Errorf("java-not-found", "%s -version failed", path).WithCause("java", err)
	}
	return parseMajor(append(stderr.Bytes(), stdout.Bytes()...))
}

func parseMajor(output []byte) (int, error) {
	m := versionLine.FindSubmatch(output)
	if m == nil {
		return 0, out.Errorf("java-version", "could not read a version from java -version")
	}
	parts := strings.Split(string(m[1]), ".")
	if parts[0] == "1" && len(parts) > 1 {
		parts = parts[1:]
	}
	major, err := strconv.Atoi(strings.SplitN(parts[0], "-", 2)[0])
	if err != nil {
		return 0, out.Errorf("java-version", "could not parse java version %q", string(m[1]))
	}
	return major, nil
}
