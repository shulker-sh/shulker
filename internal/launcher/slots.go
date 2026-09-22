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

	"shulker.sh/shulker/internal/config"
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
	// Unreproducible are the Tokens shulker can't reproduce, which adoption warns about.
	Unreproducible []string
	// UsesShim marks a launcher with no command slots at all, where what shulker fills is the profile's
	// Java instead, with a shim that hands the launch back to it.
	UsesShim bool
	// Quote quotes one word of a command the way the launcher splits it. Nil means CommandArg's
	// plain double quotes, which every other parser reads alike.
	Quote func(word, goos string) string
}

func (s Slot) quote(word, goos string) string {
	if s.Quote == nil {
		return CommandArg(word)
	}
	return s.Quote(word, goos)
}

func bareWord(word, _ string) string { return word }

var instTokens = []string{"INST_NAME", "INST_ID", "INST_DIR", "INST_MC_DIR"}

func slotOf(launcherName string) Slot {
	if e := Find(launcherName); e != nil && e.Slot != nil {
		return *e.Slot
	}
	return Slot{}
}

// SlotCommand is what goes in the launcher's slot: the interpreter, then the generated script. It
// reaches the script through the launcher's own token where there is one, so a moved instance folder
// keeps working, and through an absolute path where the launcher substitutes nothing.
func SlotCommand(launcherName, dir string, kind HookKind) string {
	return slotCommand(launcherName, dir, kind, runtime.GOOS)
}

// A literal path is quoted the way the launcher splits the command; a path through a token gets the
// plain quoted shape every parser reads alike.
func slotCommand(launcherName, dir string, kind HookKind, goos string) string {
	s := slotOf(launcherName)
	base := dir
	if s.Token != "" {
		base = s.Token
	}
	if goos == "windows" {
		return `cmd /c "` + base + `\` + instance.Dir + `\` + string(kind) + `.cmd"`
	}
	script := base + "/" + instance.Dir + "/" + string(kind)
	if s.Token != "" {
		return `sh "` + script + `"`
	}
	return "sh " + s.quote(script, goos)
}

// IsShulkerSlot reports whether a slot command is one of shulker's generated hook scripts rather
// than a player's command.
func IsShulkerSlot(command string) bool {
	for _, kind := range []HookKind{HookPreLaunch, HookPostExit} {
		if strings.Contains(command, instance.Dir+"/"+string(kind)) ||
			strings.Contains(command, instance.Dir+`\`+string(kind)) {
			return true
		}
	}
	return false
}

// Slots are what one instance holds in the slots its launcher has: two commands, or the profile's
// Java where the launcher runs no commands, and the launch command's own prefix.
//
// Shulker owns the command slots, so an empty one clears what it wrote there. It never owns the
// wrapper slot: an empty Wrapper leaves whatever the launcher has, because the instance naming no
// wrapper means the player's own setting stands.
type Slots struct {
	PreLaunch string
	PostExit  string
	Java      string
	Wrapper   string
}

// WrapperCommand is settings.wrapper as one string for a launcher's own wrapper slot. Each launcher
// splits that string itself: Prism and GDLauncher read quotes, so a word that needs them gets them,
// while ATLauncher splits on whitespace and ignores quotes, so its words stay bare and a wrapper
// word holding a space can't be written there at all.
func WrapperCommand(launcherName string, words []string) string {
	return wrapperCommand(launcherName, words, runtime.GOOS)
}

func wrapperCommand(launcherName string, words []string, goos string) string {
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		quoted = append(quoted, wrapperWord(launcherName, word, goos))
	}
	return strings.Join(quoted, " ")
}

func wrapperWord(launcherName, word, goos string) string {
	if !strings.ContainsAny(word, " \t\"'\\") {
		return word
	}
	return slotOf(launcherName).quote(word, goos)
}

// ReadSlots is what a launcher currently has in an instance's slots, and whether the instance exists
// at all. A launcher shulker fills no slots for reports nothing found.
func ReadSlots(e *Entry, in config.Instance) (Slots, bool, error) {
	if e.readSlots == nil {
		return Slots{}, false, nil
	}
	return e.readSlots(e, in)
}

func readPrismSlots(e *Entry, in config.Instance) (Slots, bool, error) {
	values, err := readINI(filepath.Join(e.InstanceDir(in.Dir), PrismInstanceFile), prismUnescape)
	if errors.Is(err, os.ErrNotExist) {
		return Slots{}, false, nil
	}
	if err != nil {
		return Slots{}, false, err
	}
	return Slots{PreLaunch: values["PreLaunchCommand"], PostExit: values["PostExitCommand"], Wrapper: values["WrapperCommand"]}, true, nil
}

func readMultiMCSlots(e *Entry, in config.Instance) (Slots, bool, error) {
	values, err := readINI(filepath.Join(e.InstanceDir(in.Dir), MultiMCInstanceFile), multimcUnescape)
	if errors.Is(err, os.ErrNotExist) {
		return Slots{}, false, nil
	}
	if err != nil {
		return Slots{}, false, err
	}
	return Slots{PreLaunch: values["PreLaunchCommand"], PostExit: values["PostExitCommand"], Wrapper: values["WrapperCommand"]}, true, nil
}

func readATLauncherSlots(e *Entry, in config.Instance) (Slots, bool, error) {
	settings, _, found, err := atlauncherSettings(e.InstanceDir(in.Dir))
	if err != nil || !found {
		return Slots{}, found, err
	}
	return Slots{PreLaunch: jsonStringValue(settings["preLaunchCommand"]), PostExit: jsonStringValue(settings["postExitCommand"]), Wrapper: jsonStringValue(settings["wrapperCommand"])}, true, nil
}

func readGDLauncherSlots(e *Entry, in config.Instance) (Slots, bool, error) {
	top, found, err := gdlauncherTop(e.InstanceDir(in.Dir))
	if err != nil || !found {
		return Slots{}, found, err
	}
	return Slots{PreLaunch: jsonStringValue(top["pre_launch_hook"]), PostExit: jsonStringValue(top["post_exit_hook"]), Wrapper: jsonStringValue(top["wrapper_command"])}, true, nil
}

func readMojangSlots(_ *Entry, in config.Instance) (Slots, bool, error) {
	java, found, err := (&Mojang{Dir: in.LauncherDir}).JavaDir(in.Dir)
	return Slots{Java: java}, found, err
}

// WriteSlots puts commands in an instance's slots, an empty one clearing that slot, except for the
// wrapper, which is only ever filled.
func WriteSlots(e *Entry, in config.Instance, s Slots) error {
	if e.writeSlots == nil {
		return nil
	}
	return e.writeSlots(e, in, s)
}

func writeMojangSlots(_ *Entry, in config.Instance, s Slots) error {
	return (&Mojang{Dir: in.LauncherDir}).SetJavaDir(in.Dir, s.Java)
}

func writePrismSlots(e *Entry, in config.Instance, s Slots) error {
	path := filepath.Join(e.InstanceDir(in.Dir), PrismInstanceFile)
	lines, err := readINILines(path)
	if err != nil {
		return err
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
	if s.Wrapper != "" {
		set["WrapperCommand"] = s.Wrapper
	}
	// Prism reads all three from the instance only with this on (BaseInstance.cpp registers them
	// against it), so a wrapper needs it as much as a command does.
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
				fmt.Fprintf(&buf, "%s=%s\n", key, prismEscape(value))
				done[key] = true
				continue
			}
		}
		buf.WriteString(line + "\n")
	}
	for _, key := range []string{"OverrideCommands", "PreLaunchCommand", "PostExitCommand", "WrapperCommand"} {
		if value, has := set[key]; has && !done[key] {
			fmt.Fprintf(&buf, "%s=%s\n", key, prismEscape(value))
		}
	}
	return fsutil.Write(path, buf.Bytes())
}

func writeMultiMCSlots(e *Entry, in config.Instance, s Slots) error {
	path := filepath.Join(e.InstanceDir(in.Dir), MultiMCInstanceFile)
	lines, err := readINILines(path)
	if err != nil {
		return err
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
	if s.Wrapper != "" {
		set["WrapperCommand"] = s.Wrapper
	}
	// MultiMC reads all three from the instance only with this on, so a wrapper needs it as much as
	// a command does.
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
				fmt.Fprintf(&buf, "%s=%s\n", key, multimcEscape(value))
				done[key] = true
				continue
			}
		}
		buf.WriteString(line + "\n")
	}
	for _, key := range []string{"OverrideCommands", "PreLaunchCommand", "PostExitCommand", "WrapperCommand"} {
		if value, has := set[key]; has && !done[key] {
			fmt.Fprintf(&buf, "%s=%s\n", key, multimcEscape(value))
		}
	}
	return fsutil.Write(path, buf.Bytes())
}

func writeATLauncherSlots(e *Entry, in config.Instance, s Slots) error {
	instanceDir := e.InstanceDir(in.Dir)
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
	if s.Wrapper != "" {
		settings["wrapperCommand"] = jsonString(s.Wrapper)
	}
	// enableCommands is per instance, so reconcile has to set it or a generated or adopted command
	// silently never runs. It gates the wrapper too, so a wrapper already in the file keeps it on.
	_, wrapped := settings["wrapperCommand"]
	if s.PreLaunch != "" || s.PostExit != "" || wrapped {
		settings["enableCommands"] = json.RawMessage("true")
	} else {
		delete(settings, "enableCommands")
	}
	if top["launcher"], err = json.Marshal(settings); err != nil {
		return err
	}
	return fsutil.WriteJSON(filepath.Join(instanceDir, ATLauncherInstanceFile), top)
}

func writeGDLauncherSlots(e *Entry, in config.Instance, s Slots) error {
	instanceDir := e.InstanceDir(in.Dir)
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
	if s.Wrapper != "" {
		top["wrapper_command"] = jsonString(s.Wrapper)
	}
	return fsutil.WriteJSON(filepath.Join(instanceDir, GDLauncherInstanceFile), top)
}

// ReleaseSlots hands an instance's slots back to its launcher: a command shulker adopted returns to
// the slot it came from, a command shulker never adopted is left exactly where it is, shulker's own
// slots are cleared, and both generated scripts go. It reports which slots shulker was holding.
func ReleaseSlots(e *Entry, in config.Instance) (tookPreLaunch, tookPostExit bool, err error) {
	gameDir := in.Dir
	current, found, err := ReadSlots(e, in)
	if err != nil {
		return false, false, err
	}
	var adopted Slots
	f, ferr := instance.Load(gameDir)
	if ferr == nil && f.Settings.Commands != nil {
		adopted = Slots{PreLaunch: f.Settings.Commands.PreLaunch, PostExit: f.Settings.Commands.PostExit}
	}
	tookPreLaunch, tookPostExit = IsShulkerSlot(current.PreLaunch), IsShulkerSlot(current.PostExit)
	if e.Slot != nil && e.Slot.UsesShim {
		// The profile keeps a Java shulker didn't set; the one it did set goes back to what the
		// launcher had, which is usually no key at all.
		if found && IsShulkerShim(current.Java) {
			var launcherJava string
			if ferr == nil && f.Resolved != nil {
				launcherJava = f.Resolved.LauncherJava
			}
			if err := WriteSlots(e, in, Slots{Java: launcherJava}); err != nil {
				return false, false, err
			}
		}
	} else if found {
		if err := WriteSlots(e, in, Slots{
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
	if err := RemoveShim(gameDir); err != nil {
		return false, false, err
	}
	if ferr == nil && (f.Settings.Commands != nil || (f.Resolved != nil && f.Resolved.LauncherJava != "")) {
		f.Settings.Commands = nil
		if f.Resolved != nil {
			f.Resolved.LauncherJava = ""
		}
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
	top, found, err = readJSONObject(path)
	if err != nil || !found {
		return nil, nil, found, err
	}
	if settings, err = jsonObjectAt(path, top, "launcher"); err != nil {
		return nil, nil, true, err
	}
	return settings, top, true, nil
}

func gdlauncherTop(instanceDir string) (map[string]json.RawMessage, bool, error) {
	return readJSONObject(filepath.Join(instanceDir, GDLauncherInstanceFile))
}

func jsonStringValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

func CommandArg(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
