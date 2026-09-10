package server

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/andrewmast/shulker/internal/mcver"
	"github.com/andrewmast/shulker/internal/out"
)

type Java struct {
	Path  string `json:"path"`
	Major int    `json:"major"`
}

var versionLine = regexp.MustCompile(`version "([^"]+)"`)

func JavaAt(home string) (Java, error) {
	bin := filepath.Join(home, "bin", "java")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return Java{}, out.Errorf("java-not-found", "no java executable under %s (looked for %s)", home, bin)
	}
	major, err := javaMajor(bin)
	if err != nil {
		return Java{}, err
	}
	return Java{Path: bin, Major: major}, nil
}

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
		return Java{}, out.Errorf("java-not-found", "no java on PATH; install a JDK or set \"java\" in shulker.json to a JDK path")
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
		return out.Errorf("java-version", "java at %s is version %d; this Minecraft version needs Java %d or newer. Set \"java\" in shulker.json to a JDK path or put a newer java on PATH", j.Path, j.Major, required)
	}
	return nil
}

func javaMajor(path string) (int, error) {
	var stderr, stdout bytes.Buffer
	cmd := exec.Command(path, "-version")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return 0, out.Errorf("java-not-found", "%s -version failed: %v", path, err)
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
