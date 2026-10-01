package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// List is the processes this user is running, read from /proc. A process whose executable link
// can't be read is named by the first word of its command line.
func List() ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	uid := uint32(os.Getuid())
	var processes []Process
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", entry.Name())
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uid {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		argv := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		exe, err := os.Readlink(filepath.Join(dir, "exe"))
		if err != nil {
			exe = argv[0]
		}
		processes = append(processes, Process{Exe: exe, Command: strings.Join(argv, " ")})
	}
	return processes, nil
}
