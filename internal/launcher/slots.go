package launcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/instance"
)

// Slot is how one launcher's command slots behave: what it substitutes into a command it runs, and
// whether shulker has to bound its own work to get a message past the launcher.
type Slot struct {
	// Token names the directory holding .shulker/ in the launcher's own variables. Empty means the
	// launcher substitutes nothing, so the slot command carries an absolute path instead.
	Token string
	// Tokens are the variables the launcher substitutes into a slot command. It stops substituting
	// them once the command lives inside the generated script, so the script exports them.
	Tokens []string
	// Deadline bounds the pre-launch refresh. Only GDLauncher sets one: it discards hook output on
	// its own 300s timeout, so shulker stops first to explain itself.
	Deadline string
	// Reproducible is Tokens minus the ones shulker cannot reproduce, which adoption warns about.
	Unreproducible []string
}

var instTokens = []string{"INST_NAME", "INST_ID", "INST_DIR", "INST_MC_DIR"}

var slots = map[string]Slot{
	"prism":      {Token: "$INST_MC_DIR", Tokens: instTokens},
	"multimc":    {Token: "$INST_MC_DIR", Tokens: instTokens},
	"atlauncher": {Token: "$INST_DIR", Tokens: instTokens, Unreproducible: []string{"INST_JAVA", "INST_JAVA_ARGS"}},
	"gdlauncher": {Deadline: "4m"},
}

// SlotOf is how a launcher's slots behave, and whether shulker fills them at all.
func SlotOf(name string) (Slot, bool) {
	s, ok := slots[name]
	return s, ok
}

// SlotCommand is what goes in the launcher's slot: the interpreter, then the generated script. It
// reaches the script through the launcher's own token where there is one, so a moved instance folder
// keeps working, and through an absolute path where the launcher substitutes nothing.
func SlotCommand(launcherName, dir string, kind HookKind) string {
	return slotCommand(launcherName, dir, kind, runtime.GOOS)
}

// GDLauncher is the one launcher that sees a literal path here, and it splits the command itself, so
// its path is quoted the way its parser reads; the others get the plain quoted shape every parser
// reads alike.
func slotCommand(launcherName, dir string, kind HookKind, goos string) string {
	base := dir
	if s, ok := slots[launcherName]; ok && s.Token != "" {
		base = s.Token
	}
	if goos == "windows" {
		return `cmd /c "` + base + `\` + instance.Dir + `\` + string(kind) + `.cmd"`
	}
	script := base + "/" + instance.Dir + "/" + string(kind)
	if launcherName == "gdlauncher" {
		return "sh " + gdlauncherHookArg(script, goos)
	}
	return `sh "` + script + `"`
}

// IsShulkerSlot reports whether a slot command is one shulker owns, which is how reconcile tells its
// own slot from a command to adopt, and how link tells its own instance from a player's.
//
// Two shapes count. The generated script is what shulker writes now. The inline `sync --into` command
// is what shulker wrote before the scripts existed, and it is still shulker's: treating it as a
// stranger's would refuse to link over an instance shulker made, and would leave that command in
// place while adopting it.
func IsShulkerSlot(command string) bool {
	for _, kind := range []HookKind{HookPreLaunch, HookPostExit} {
		if strings.Contains(command, instance.Dir+"/"+string(kind)) ||
			strings.Contains(command, instance.Dir+`\`+string(kind)) {
			return true
		}
	}
	return isLegacySyncCommand(command)
}

// isLegacySyncCommand recognises the pre-script slot command. GDLauncher ran hooks in the game
// directory without setting variables, so its sync went into ".".
func isLegacySyncCommand(command string) bool {
	return strings.Contains(command, " sync ") &&
		(strings.HasSuffix(command, `--into "$INST_MC_DIR"`) || strings.HasSuffix(command, " --into ."))
}

// Slots are the commands in one instance's two slots.
type Slots struct {
	PreLaunch string
	PostExit  string
}

// ReadSlots is what a launcher currently has in an instance's slots, and whether the instance exists
// at all. A launcher shulker fills no slots for reports nothing found.
func ReadSlots(e *Entry, instanceDir string) (Slots, bool, error) {
	switch e.Name {
	case "prism", "multimc":
		values, err := readINI(filepath.Join(instanceDir, InstanceConfigFile), e.Name == "multimc")
		if errors.Is(err, os.ErrNotExist) {
			return Slots{}, false, nil
		}
		if err != nil {
			return Slots{}, false, err
		}
		return Slots{PreLaunch: values["PreLaunchCommand"], PostExit: values["PostExitCommand"]}, true, nil
	case "atlauncher":
		settings, _, found, err := atlauncherSettings(instanceDir)
		if err != nil || !found {
			return Slots{}, found, err
		}
		return Slots{PreLaunch: jsonStringValue(settings["preLaunchCommand"]), PostExit: jsonStringValue(settings["postExitCommand"])}, true, nil
	case "gdlauncher":
		top, found, err := gdlauncherTop(instanceDir)
		if err != nil || !found {
			return Slots{}, found, err
		}
		return Slots{PreLaunch: jsonStringValue(top["pre_launch_hook"]), PostExit: jsonStringValue(top["post_exit_hook"])}, true, nil
	}
	return Slots{}, false, nil
}

// WriteSlots puts commands in an instance's slots, an empty one clearing that slot. It is what
// reconcile uses, so a hand-edited switch takes effect through the same path that first set it.
func WriteSlots(e *Entry, instanceDir string, s Slots) error {
	switch e.Name {
	case "prism", "multimc":
		return writePrismSlots(instanceDir, e.Name == "multimc", s)
	case "atlauncher":
		return writeATLauncherSlots(instanceDir, s)
	case "gdlauncher":
		return writeGDLauncherSlots(instanceDir, s)
	}
	return nil
}

func writePrismSlots(instanceDir string, multimc bool, s Slots) error {
	path := filepath.Join(instanceDir, InstanceConfigFile)
	lines, err := readINILines(path)
	if err != nil {
		return err
	}
	escape := iniEscape
	if multimc {
		escape = multimcEscape
	}
	set := map[string]string{}
	remove := map[string]bool{}
	for key, command := range map[string]string{"PreLaunchCommand": s.PreLaunch, "PostExitCommand": s.PostExit} {
		if command == "" {
			remove[key] = true
			continue
		}
		set[key] = command
	}
	if len(set) > 0 {
		set["OverrideCommands"] = "true"
	}
	var buf bytes.Buffer
	if len(lines) == 0 {
		lines = []string{"[General]"}
	}
	done := map[string]bool{}
	for _, line := range lines {
		if key, _, ok := splitINILine(line); ok {
			if remove[key] {
				continue
			}
			if value, has := set[key]; has {
				fmt.Fprintf(&buf, "%s=%s\n", key, escape(value))
				done[key] = true
				continue
			}
		}
		buf.WriteString(line + "\n")
	}
	for _, key := range []string{"OverrideCommands", "PreLaunchCommand", "PostExitCommand"} {
		if value, has := set[key]; has && !done[key] {
			fmt.Fprintf(&buf, "%s=%s\n", key, escape(value))
		}
	}
	return fsutil.Write(path, buf.Bytes())
}

func writeATLauncherSlots(instanceDir string, s Slots) error {
	settings, top, found, err := atlauncherSettings(instanceDir)
	if err != nil || !found {
		return err
	}
	for key, command := range map[string]string{"preLaunchCommand": s.PreLaunch, "postExitCommand": s.PostExit} {
		if command == "" {
			delete(settings, key)
			continue
		}
		settings[key] = jsonString(command)
	}
	// enableCommands is per instance, so reconcile has to set it or a generated or adopted command
	// silently never runs.
	if s.PreLaunch != "" || s.PostExit != "" {
		settings["enableCommands"] = json.RawMessage("true")
	} else {
		delete(settings, "enableCommands")
	}
	if top["launcher"], err = json.Marshal(settings); err != nil {
		return err
	}
	return fsutil.WriteJSON(filepath.Join(instanceDir, ATLauncherInstanceFile), top)
}

func writeGDLauncherSlots(instanceDir string, s Slots) error {
	top, found, err := gdlauncherTop(instanceDir)
	if err != nil || !found {
		return err
	}
	for key, command := range map[string]string{"pre_launch_hook": s.PreLaunch, "post_exit_hook": s.PostExit} {
		if command == "" {
			delete(top, key)
			continue
		}
		top[key] = jsonString(command)
	}
	return fsutil.WriteJSON(filepath.Join(instanceDir, GDLauncherInstanceFile), top)
}

// ReleaseSlots hands an instance's slots back to its launcher: a command shulker adopted returns to
// the slot it came from, a command shulker never adopted is left exactly where it is, shulker's own
// slots are cleared, and both generated scripts go. It reports which slots shulker was holding, so
// the caller can say what it removed. Used by `unlink` and by a link that switches to symlink mode,
// so letting go of a slot happens one way.
func ReleaseSlots(e *Entry, gameDir string) (tookPreLaunch, tookPostExit bool, err error) {
	instanceDir := e.InstanceDir(gameDir)
	current, found, err := ReadSlots(e, instanceDir)
	if err != nil {
		return false, false, err
	}
	var adopted Slots
	f, ferr := instance.Load(gameDir)
	if ferr == nil && f.Settings.Commands != nil {
		adopted = Slots{PreLaunch: f.Settings.Commands.PreLaunch, PostExit: f.Settings.Commands.PostExit}
	}
	tookPreLaunch, tookPostExit = IsShulkerSlot(current.PreLaunch), IsShulkerSlot(current.PostExit)
	if found {
		if err := WriteSlots(e, instanceDir, Slots{
			PreLaunch: releaseSlot(adopted.PreLaunch, current.PreLaunch),
			PostExit:  releaseSlot(adopted.PostExit, current.PostExit),
		}); err != nil {
			return false, false, err
		}
	}
	for _, kind := range []HookKind{HookPreLaunch, HookPostExit} {
		if err := RemoveHook(gameDir, kind); err != nil {
			return false, false, err
		}
	}
	if ferr == nil && f.Settings.Commands != nil {
		f.Settings.Commands = nil
		if err := f.Save(gameDir); err != nil {
			return false, false, err
		}
	}
	return tookPreLaunch, tookPostExit, nil
}

// releaseSlot is what one slot holds once shulker lets go: the command shulker adopted when it took
// the slot, else a command it never adopted and so must not destroy, else nothing.
func releaseSlot(adopted, current string) string {
	if adopted != "" {
		return adopted
	}
	if current != "" && !IsShulkerSlot(current) {
		return current
	}
	return ""
}

func atlauncherSettings(instanceDir string) (settings, top map[string]json.RawMessage, found bool, err error) {
	path := filepath.Join(instanceDir, ATLauncherInstanceFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	top = map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil, true, fmt.Errorf("%s: %w", path, err)
	}
	settings = map[string]json.RawMessage{}
	if raw, ok := top["launcher"]; ok {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return nil, nil, true, fmt.Errorf("%s: launcher: %w", path, err)
		}
	}
	return settings, top, true, nil
}

func gdlauncherTop(instanceDir string) (map[string]json.RawMessage, bool, error) {
	path := filepath.Join(instanceDir, GDLauncherInstanceFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, true, fmt.Errorf("%s: %w", path, err)
	}
	return top, true, nil
}

func jsonStringValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}
