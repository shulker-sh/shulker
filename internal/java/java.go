// Package java finds the Java shulker launches the game and its servers with: a runtime from
// Mojang's manifest for the version's component, kept under the cache, or a Java already on the
// machine, checked against the major version the lock needs.
package java

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

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/version/minecraft"
)

// Java is a java executable and the major version it reports.
type Binary struct {
	Path  string `json:"path"`
	Major int    `json:"major"`
}

var versionLine = regexp.MustCompile(`version "([^"]+)"`)

// Bin is the java executable under a runtime home.
func Bin(home string) string {
	bin := filepath.Join(home, "bin", "java")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	return bin
}

// At is the java under a Java home.
func At(home string) (Binary, error) {
	bin := Bin(home)
	if _, err := exec.LookPath(bin); err != nil {
		return Binary{}, out.Errorf("java-not-found", "no java executable under %s (looked for %s)", home, bin)
	}
	major, err := javaMajor(bin)
	if err != nil {
		return Binary{}, err
	}
	return Binary{Path: bin, Major: major}, nil
}

// Client is the java a `java` setting names: the binary at path, or the one under it when path
// is a Java home. It is refused when it is older than the Minecraft version needs.
func Client(path string, required int) (Binary, error) {
	bin := path
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		bin = Bin(path)
	}
	if _, err := exec.LookPath(bin); err != nil {
		e := out.Errorf("java-not-found", "no java executable at %s", path)
		e.Help = "the java setting takes the path of a java binary or a Java home"
		return Binary{}, e
	}
	major, err := javaMajor(bin)
	if err != nil {
		return Binary{}, err
	}
	j := Binary{Path: bin, Major: major}
	return j, requireMajor(j, required)
}

// Find picks a server's java. An absolute override is a java binary or a Java home; any other
// override is a version range the java on PATH must fall in, and without one it must reach required.
func Find(override string, required int) (Binary, error) {
	if filepath.IsAbs(override) {
		return Client(override, required)
	}
	path, err := exec.LookPath("java")
	if err != nil {
		e := out.Errorf("java-not-found", "no java on PATH")
		e.Help = "install a JDK or set \"java\" in shulker.json to a JDK path"
		return Binary{}, e
	}
	major, err := javaMajor(path)
	if err != nil {
		return Binary{}, err
	}
	j := Binary{Path: path, Major: major}
	if override != "" {
		r, err := minecraft.ParseRange(override)
		if err != nil {
			return j, out.Errorf("java-range", "manifest java %q is neither an absolute path nor a version range", override)
		}
		if !r.Contains(minecraft.MustParse(fmt.Sprintf("%d.0.0", major))) {
			return j, out.Errorf("java-version", "java at %s is version %d, outside the manifest range %q", path, major, override)
		}
		return j, nil
	}
	return j, requireMajor(j, required)
}

func requireMajor(j Binary, required int) error {
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

func (j Binary) String() string { return fmt.Sprintf("Java %d (%s)", j.Major, j.Path) }
