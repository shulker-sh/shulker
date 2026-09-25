package server

import (
	"fmt"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

const (
	FlagsAikars = "aikars"
	FlagsNone   = "none"
	largeHeapMB = 12 * 1024
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

// JVMArgs are a server's heap flags, its preset's tuning flags and the author's own, in that order.
func JVMArgs(memory, preset string, extra []string) ([]string, error) {
	if memory == "" {
		memory = manifest.DefaultServerMemory
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
		e := out.Errorf("jvm-flags", "unknown jvmFlags preset %q", preset)
		e.Help = fmt.Sprintf("use %q or %q", FlagsAikars, FlagsNone)
		return nil, e
	}
	return append(args, extra...), nil
}

func memoryMB(memory string) (int, error) {
	unit := strings.ToUpper(memory[len(memory)-1:])
	n, err := strconv.Atoi(memory[:len(memory)-1])
	if err != nil || n <= 0 || (unit != "M" && unit != "G") {
		e := out.Errorf("memory", "server memory %q must be a whole number of M or G", memory)
		e.Help = fmt.Sprintf("for example %q", manifest.DefaultServerMemory)
		return 0, e
	}
	if unit == "G" {
		n *= 1024
	}
	return n, nil
}

// Command is the java arguments that start a server, run without its GUI.
func Command(jvmArgs, launch []string) []string {
	return append(append(append([]string{}, jvmArgs...), launch...), "--nogui")
}
