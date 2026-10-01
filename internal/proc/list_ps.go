//go:build !windows && !linux

package proc

import (
	"os/exec"
	"strings"
)

// List is the processes this user is running. ps prints a path that may hold spaces, so the
// executable and the command line are asked for apart and matched up by pid. ps is named by its
// path, since a launcher's hook runs with almost no PATH.
func List() ([]Process, error) {
	exes, err := psColumn("comm=")
	if err != nil {
		return nil, err
	}
	commands, err := psColumn("args=")
	if err != nil {
		return nil, err
	}
	processes := make([]Process, 0, len(exes))
	for pid, exe := range exes {
		processes = append(processes, Process{Exe: exe, Command: commands[pid]})
	}
	return processes, nil
}

func psColumn(column string) (map[string]string, error) {
	data, err := exec.Command("/bin/ps", "-xww", "-o", "pid=,"+column).Output()
	if err != nil {
		return nil, err
	}
	return parsePS(string(data)), nil
}

func parsePS(output string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if pid, value, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			values[pid] = strings.TrimSpace(value)
		}
	}
	return values
}
