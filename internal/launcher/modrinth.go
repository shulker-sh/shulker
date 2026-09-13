package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"shulker.sh/shulker/internal/config"
)

const (
	ModrinthProfilesDir = "profiles"
	ModrinthDBFile      = "app.db"
)

var ErrInstanceNotCreated = errors.New("no instance was created")

// Modrinth App keeps instance metadata only in its SQLite database, so an
// instance is created by handing the app an .mrpack; its folder under
// profiles/ is the game directory and appears once the app has taken the pack.
type Modrinth struct {
	Dir     string
	Open    func(path string) error
	Timeout time.Duration
	Poll    time.Duration
}

func DefaultModrinthDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "ModrinthApp"), nil
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", errors.New("APPDATA is not set")
		}
		return filepath.Join(appdata, "ModrinthApp"), nil
	default:
		if data := os.Getenv("XDG_DATA_HOME"); data != "" {
			return filepath.Join(data, "ModrinthApp"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "ModrinthApp"), nil
	}
}

func (m *Modrinth) Check() error {
	info, err := os.Stat(m.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w at %s", ErrNotFound, m.Dir)
	}
	return nil
}

func (m *Modrinth) ProfilesDir() string { return filepath.Join(m.Dir, ModrinthProfilesDir) }

var modrinthFolderChars = strings.NewReplacer(
	"/", "_", `\`, "_", "?", "_", "*", "_", ":", "_", "'", "_", `"`, "_", "|", "_", "<", "_", ">", "_", "!", "_",
)

// InstanceFolder is the folder the app makes for an instance name when nothing
// else has taken it; a taken name gets " (1)", " (2)" and so on.
func (m *Modrinth) InstanceFolder(name string) string {
	return filepath.Join(m.ProfilesDir(), modrinthFolderChars.Replace(name))
}

func (m *Modrinth) Instances() (map[string]bool, error) {
	entries, err := os.ReadDir(m.ProfilesDir())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names[e.Name()] = true
		}
	}
	return names, nil
}

func (m *Modrinth) OpenMrpack(path string) error {
	if m.Open != nil {
		return m.Open(path)
	}
	return OpenWithModrinth(path)
}

func OpenWithModrinth(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-a", "Modrinth App", path)
	case "windows":
		exe := filepath.Join(os.Getenv("LOCALAPPDATA"), "Modrinth App", "Modrinth App.exe")
		if _, err := os.Stat(exe); err == nil {
			return exec.Command(exe, path).Start()
		}
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s: %s", err, msg)
		}
		return err
	}
	return nil
}

// WaitForInstance returns the folder the app created after before was taken,
// preferring want when several appeared.
func (m *Modrinth) WaitForInstance(before map[string]bool, want string) (string, error) {
	timeout, poll := m.Timeout, m.Poll
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	if poll == 0 {
		poll = 500 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		now, err := m.Instances()
		if err != nil {
			return "", err
		}
		var added []string
		for name := range now {
			if !before[name] {
				added = append(added, name)
			}
		}
		if len(added) > 0 {
			sort.Strings(added)
			pick := added[0]
			for _, name := range added {
				if filepath.Join(m.ProfilesDir(), name) == want {
					pick = name
				}
			}
			return filepath.Join(m.ProfilesDir(), pick), nil
		}
		if time.Now().After(deadline) {
			return "", ErrInstanceNotCreated
		}
		time.Sleep(poll)
	}
}

func relinkModrinth(e *Entry, l config.Link) (args []string, in string) {
	args = []string{"shulker", "link", e.Name, ShellArg(l.Source)}
	if l.Ref != "" {
		args = append(args, "--ref", ShellArg(l.Ref))
	}
	return append(args, "--target", ShellArg(l.Target), "--name", ShellArg(l.Name)), ""
}

func forgetModrinth(e *Entry, l config.Link) (Forgotten, error) {
	if _, err := os.Stat(l.Dir); errors.Is(err, os.ErrNotExist) {
		return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); its instance was already gone.", l.Name, e.Title)}, nil
	}
	return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); the instance stays in the app, delete it there if you no longer want it.", l.Name, e.Title)}, nil
}

func detectModrinth(gameDir string) (dir string, ok bool) {
	profiles := filepath.Dir(gameDir)
	if filepath.Base(profiles) != ModrinthProfilesDir {
		return "", false
	}
	dir = filepath.Dir(profiles)
	if _, err := os.Stat(filepath.Join(dir, ModrinthDBFile)); err != nil {
		return "", false
	}
	return dir, true
}
