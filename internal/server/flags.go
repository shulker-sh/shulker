package server

import (
	"fmt"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/out"
)

const (
	DefaultMemory = "4G"
	FlagsAikars   = "aikars"
	FlagsNone     = "none"
	largeHeapMB   = 12 * 1024
)

var aikarsCommon = []string{
	"-XX:+UseG1GC",
	"-XX:+ParallelRefProcEnabled",
	"-XX:MaxGCPauseMillis=200",
	"-XX:+UnlockExperimentalVMOptions",
	"-XX:+DisableExplicitGC",
	"-XX:+AlwaysPreTouch",
	"-XX:G1HeapWastePercent=5",
	"-XX:G1MixedGCCountTarget=4",
	"-XX:G1MixedGCLiveThresholdPercent=90",
	"-XX:G1RSetUpdatingPauseTimePercent=5",
	"-XX:SurvivorRatio=32",
	"-XX:+PerfDisableSharedMem",
	"-XX:MaxTenuringThreshold=1",
	"-Dusing.aikars.flags=https://mcflags.emc.gs",
	"-Daikars.new.flags=true",
}

var aikarsSmall = []string{
	"-XX:G1NewSizePercent=30",
	"-XX:G1MaxNewSizePercent=40",
	"-XX:G1HeapRegionSize=8M",
	"-XX:G1ReservePercent=20",
	"-XX:InitiatingHeapOccupancyPercent=15",
}

var aikarsLarge = []string{
	"-XX:G1NewSizePercent=40",
	"-XX:G1MaxNewSizePercent=50",
	"-XX:G1HeapRegionSize=16M",
	"-XX:G1ReservePercent=15",
	"-XX:InitiatingHeapOccupancyPercent=20",
}

func JVMArgs(memory, preset string, extra []string) ([]string, error) {
	if memory == "" {
		memory = DefaultMemory
	}
	mb, err := memoryMB(memory)
	if err != nil {
		return nil, err
	}
	args := []string{"-Xms" + memory, "-Xmx" + memory}
	switch preset {
	case "", FlagsAikars:
		args = append(args, aikarsCommon...)
		if mb >= largeHeapMB {
			args = append(args, aikarsLarge...)
		} else {
			args = append(args, aikarsSmall...)
		}
	case FlagsNone:
	default:
		return nil, out.Errorf("jvm-flags", "unknown jvmFlags preset %q; use %q or %q", preset, FlagsAikars, FlagsNone)
	}
	return append(args, extra...), nil
}

func memoryMB(memory string) (int, error) {
	unit := strings.ToUpper(memory[len(memory)-1:])
	n, err := strconv.Atoi(memory[:len(memory)-1])
	if err != nil || n <= 0 || (unit != "M" && unit != "G") {
		return 0, out.Errorf("memory", "server memory %q must be a whole number of M or G, e.g. %q", memory, DefaultMemory)
	}
	if unit == "G" {
		n *= 1024
	}
	return n, nil
}

func Command(jvmArgs, launch []string) []string {
	return append(append(append([]string{}, jvmArgs...), launch...), "--nogui")
}

func (j Java) String() string { return fmt.Sprintf("Java %d (%s)", j.Major, j.Path) }
