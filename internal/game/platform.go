package game

import (
	"debug/macho"
	"runtime"
)

// Platform is the machine a launch is assembled for, named the way a version JSON names it rather
// than the way Go does.
type Platform struct {
	OS   string
	Arch string
	// Version is the operating system's own version, which a handful of old rules match with a
	// regular expression. Shulker builds without cgo and has no portable way to read it, so it
	// stays empty and a rule that names a version never matches.
	Version string
}

// Host is the platform shulker is running on.
func Host() Platform { return Platform{OS: osName(runtime.GOOS), Arch: archName(runtime.GOARCH)} }

func osName(goos string) string {
	switch goos {
	case "darwin":
		return "osx"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

func archName(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	case "arm":
		return "arm32"
	default:
		return goarch
	}
}

// Classifier is the "<os>-<arch>" key a native library may be filed under.
func (p Platform) Classifier() string { return p.OS + "-" + p.Arch }

// IsLegacyArch reports whether this is one of the two architectures Mojang assumed were the only
// ones, which is when a classifier may name the operating system alone.
func (p Platform) IsLegacyArch() bool { return p.Arch == "x86_64" || p.Arch == "x86" }

// ForJava is the platform a launch with the java binary at path runs on: the host, except on a Mac
// whose java is built only for another architecture, as an Intel java on Apple Silicon runs under
// Rosetta and loads only Intel natives.
func ForJava(path string) Platform {
	p := Host()
	if runtime.GOOS != "darwin" {
		return p
	}
	if arch, ok := machoArch(path, p.Arch); ok {
		p.Arch = arch
	}
	return p
}

// machoArch is the architecture a Mach-O binary runs as on a host of hostArch: the host's own where
// a universal binary carries it, else the binary's first.
func machoArch(path, hostArch string) (string, bool) {
	if fat, err := macho.OpenFat(path); err == nil {
		defer fat.Close()
		for _, a := range fat.Arches {
			if machoCPU(a.Cpu) == hostArch {
				return hostArch, true
			}
		}
		if len(fat.Arches) > 0 {
			return machoCPU(fat.Arches[0].Cpu), true
		}
		return "", false
	}
	f, err := macho.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	return machoCPU(f.Cpu), true
}

func machoCPU(cpu macho.Cpu) string {
	switch cpu {
	case macho.CpuAmd64:
		return "x86_64"
	case macho.Cpu386:
		return "x86"
	case macho.CpuArm64:
		return "arm64"
	}
	return cpu.String()
}
