package launcher

import (
	"path"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/proc"
)

// Processes lists what is running, for telling whether a launcher is open. A test swaps it.
var Processes = proc.List

// Process is how a launcher's own process is known among those running.
type Process struct {
	// Names are the executable's file names, lower case and without .exe.
	Names []string
	// PathSuffix is the end of the executable's path, for one whose name alone says too little.
	PathSuffix string
	// JavaCommand are the words one of which a Java command line holds, for a launcher that is a
	// jar Java runs.
	JavaCommand []string
}

var javaNames = []string{"java", "javaw"}

// matches says whether p is the launcher, and whether it might be but can't be told: a Java process
// whose command line the system doesn't give.
func (m *Process) matches(p proc.Process) (is, unsure bool) {
	exe := strings.ReplaceAll(p.Exe, `\`, "/")
	name := strings.TrimSuffix(strings.ToLower(path.Base(exe)), ".exe")
	// A Nix wrapper runs the real binary as .<name>-wrapped.
	if wrapped, ok := strings.CutSuffix(name, "-wrapped"); ok {
		name = strings.TrimPrefix(wrapped, ".")
	}
	if slices.Contains(m.Names, name) || m.PathSuffix != "" && strings.HasSuffix(exe, m.PathSuffix) {
		return true, false
	}
	if len(m.JavaCommand) == 0 || !slices.Contains(javaNames, name) {
		return false, false
	}
	if p.Command == "" {
		return false, true
	}
	return slices.ContainsFunc(m.JavaCommand, func(word string) bool { return strings.Contains(p.Command, word) }), false
}

func (m *Process) running() (running, detectable bool) {
	processes, err := Processes()
	if err != nil {
		return false, false
	}
	detectable = true
	for _, p := range processes {
		is, unsure := m.matches(p)
		if is {
			return true, true
		}
		if unsure {
			detectable = false
		}
	}
	return false, detectable
}

// restartNote reminds the player to restart a launcher that only reads its instances when it
// starts, unless it is known to be closed.
func restartNote(e *Entry, why string) string {
	switch running, detectable := e.IsRunning(); {
	case running:
		return "restart " + e.Title + " " + why
	case detectable:
		return ""
	}
	return "restart " + e.Title + " if it is open " + why
}
