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

func FindJava(override string, required int) (Java, error) {
	path, err := javaPath(override)
	if err != nil {
		return Java{}, err
	}
	major, err := javaMajor(path)
	if err != nil {
		return Java{}, err
	}
	j := Java{Path: path, Major: major}
	if override != "" && !filepath.IsAbs(override) {
		r, err := mcver.ParseRange(override)
		if err != nil {
			return j, out.Errorf("java-range", "manifest java %q is neither an absolute path nor a version range", override)
		}
		if !r.Contains(mcver.MustParse(fmt.Sprintf("%d.0.0", major))) {
			return j, out.Errorf("java-version", "java at %s is version %d, outside the manifest range %q", path, major, override)
		}
		return j, nil
	}
	if major < required {
		return j, out.Errorf("java-version", "java at %s is version %d; this Minecraft version needs Java %d or newer. Set \"java\" in shulker.json to a JDK path or put a newer java on PATH", path, major, required)
	}
	return j, nil
}

func javaPath(override string) (string, error) {
	if override != "" && filepath.IsAbs(override) {
		bin := filepath.Join(override, "bin", "java")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		if _, err := exec.LookPath(bin); err != nil {
			return "", out.Errorf("java-not-found", "no java executable under manifest java path %s (looked for %s)", override, bin)
		}
		return bin, nil
	}
	path, err := exec.LookPath("java")
	if err != nil {
		return "", out.Errorf("java-not-found", "no java on PATH; install a JDK or set \"java\" in shulker.json to a JDK path")
	}
	return path, nil
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
