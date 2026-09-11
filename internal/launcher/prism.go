package launcher

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	InstanceConfigFile = "instance.cfg"
	PackFile           = "mmc-pack.json"
)

var ErrGameDirNotEmpty = errors.New("instance game directory is not empty")

var loaderUIDs = map[string]string{
	"fabric":   "net.fabricmc.fabric-loader",
	"quilt":    "org.quiltmc.quilt-loader",
	"neoforge": "net.neoforged",
	"forge":    "net.minecraftforge",
}

type Prism struct {
	Dir     string
	MultiMC bool
}

type Instance struct {
	ID            string
	Name          string
	Minecraft     string
	LoaderType    string
	LoaderVersion string
	PreLaunch     string
	GameDirLink   string
}

type InstanceResult struct {
	Dir     string
	GameDir string
	Created bool
}

func DefaultPrismDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "PrismLauncher"), nil
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", errors.New("APPDATA is not set")
		}
		return filepath.Join(appdata, "PrismLauncher"), nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "PrismLauncher"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "PrismLauncher"), nil
	}
}

func LoaderUID(loaderType string) (string, bool) {
	uid, ok := loaderUIDs[loaderType]
	return uid, ok
}

func (l *Prism) Check() error {
	info, err := os.Stat(l.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w at %s", ErrNotFound, l.Dir)
	}
	return nil
}

func (l *Prism) InstancesDir() string {
	for _, cfg := range []string{"prismlauncher.cfg", "multimc.cfg"} {
		values, err := readINI(filepath.Join(l.Dir, cfg), cfg == "multimc.cfg")
		if err != nil {
			continue
		}
		if dir := values["InstanceDir"]; dir != "" {
			if filepath.IsAbs(dir) {
				return dir
			}
			return filepath.Join(l.Dir, dir)
		}
	}
	return filepath.Join(l.Dir, "instances")
}

func (l *Prism) WriteInstance(inst Instance) (InstanceResult, error) {
	dir := filepath.Join(l.InstancesDir(), inst.ID)
	res := InstanceResult{Dir: dir}
	cfgPath := filepath.Join(dir, InstanceConfigFile)
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		res.Created = true
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	gameDir, err := l.prepareGameDir(dir, inst.GameDirLink)
	if err != nil {
		return res, err
	}
	res.GameDir = gameDir
	if err := writePack(filepath.Join(dir, PackFile), inst); err != nil {
		return res, err
	}
	return res, writeInstanceConfig(cfgPath, inst, l.MultiMC)
}

func (l *Prism) GameDir(id string) string {
	return gameDirIn(filepath.Join(l.InstancesDir(), id))
}

func gameDirIn(dir string) string {
	gameDir := filepath.Join(dir, "minecraft")
	dotDir := filepath.Join(dir, ".minecraft")
	if _, err := os.Lstat(dotDir); err == nil {
		if _, err := os.Lstat(gameDir); errors.Is(err, os.ErrNotExist) {
			return dotDir
		}
	}
	return gameDir
}

func (l *Prism) prepareGameDir(dir, link string) (string, error) {
	gameDir := gameDirIn(dir)
	info, err := os.Lstat(gameDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return "", err
	case info.Mode()&os.ModeSymlink != 0:
		if link != "" {
			if current, err := os.Readlink(gameDir); err == nil && current == link {
				return gameDir, nil
			}
		}
		if err := os.Remove(gameDir); err != nil {
			return "", err
		}
	case link != "":
		entries, err := os.ReadDir(gameDir)
		if err != nil {
			return "", err
		}
		if len(entries) > 0 {
			return gameDir, fmt.Errorf("%w: %s", ErrGameDirNotEmpty, gameDir)
		}
		if err := os.Remove(gameDir); err != nil {
			return "", err
		}
	default:
		return gameDir, nil
	}
	if link != "" {
		return gameDir, os.Symlink(link, gameDir)
	}
	return gameDir, os.Mkdir(gameDir, 0o755)
}

func writePack(path string, inst Instance) error {
	var pack struct {
		Components    []map[string]json.RawMessage `json:"components"`
		FormatVersion int                          `json:"formatVersion"`
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &pack); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	pack.FormatVersion = 1
	loaderUID, _ := LoaderUID(inst.LoaderType)
	wanted := map[string]string{"net.minecraft": inst.Minecraft, loaderUID: inst.LoaderVersion}
	var components []map[string]json.RawMessage
	seen := map[string]bool{}
	for _, c := range pack.Components {
		var uid string
		_ = json.Unmarshal(c["uid"], &uid)
		if _, other := loaderUIDs[uidLoader(uid)]; other && uid != loaderUID {
			continue
		}
		if version, ok := wanted[uid]; ok {
			c["version"] = jsonString(version)
			seen[uid] = true
		}
		components = append(components, c)
	}
	if !seen["net.minecraft"] {
		components = append([]map[string]json.RawMessage{{"important": json.RawMessage("true"), "uid": jsonString("net.minecraft"), "version": jsonString(inst.Minecraft)}}, components...)
	}
	if !seen[loaderUID] {
		components = append(components, map[string]json.RawMessage{"uid": jsonString(loaderUID), "version": jsonString(inst.LoaderVersion)})
	}
	pack.Components = components
	data, err := json.MarshalIndent(pack, "", "    ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'))
}

func uidLoader(uid string) string {
	for loader, candidate := range loaderUIDs {
		if candidate == uid {
			return loader
		}
	}
	return ""
}

func jsonString(s string) json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}

// Prism reads instance.cfg with QSettings only when ConfigVersion is present;
// otherwise it uses the MultiMC-era parser, which strips backslashes but keeps
// the surrounding quotes of a QSettings-quoted value. MultiMC only has the old
// parser, so its values are written unquoted with old-style escapes.
func writeInstanceConfig(path string, inst Instance, multimc bool) error {
	lines, err := readINILines(path)
	if err != nil {
		return err
	}
	escape := iniEscape
	if multimc {
		escape = multimcEscape
	}
	set := map[string]string{"InstanceType": "OneSix", "name": inst.Name}
	if !multimc {
		set["ConfigVersion"] = "1.3"
	}
	remove := map[string]bool{}
	if inst.PreLaunch != "" {
		set["PreLaunchCommand"] = inst.PreLaunch
		set["OverrideCommands"] = "true"
	} else {
		remove["PreLaunchCommand"] = true
	}
	var buf bytes.Buffer
	if len(lines) == 0 {
		lines = []string{"[General]"}
	}
	done := map[string]bool{}
	for _, line := range lines {
		key, _, ok := splitINILine(line)
		if ok {
			if remove[key] {
				continue
			}
			if key == "ConfigVersion" {
				done[key] = true
			} else if value, has := set[key]; has {
				fmt.Fprintf(&buf, "%s=%s\n", key, escape(value))
				done[key] = true
				continue
			}
		}
		buf.WriteString(line + "\n")
	}
	for _, key := range []string{"ConfigVersion", "InstanceType", "name", "OverrideCommands", "PreLaunchCommand"} {
		if value, has := set[key]; has && !done[key] {
			fmt.Fprintf(&buf, "%s=%s\n", key, escape(value))
		}
	}
	return writeAtomic(path, buf.Bytes())
}

func readINILines(path string) ([]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, strings.TrimRight(sc.Text(), "\r"))
	}
	return lines, sc.Err()
}

func readINI(path string, multimc bool) (map[string]string, error) {
	lines, err := readINILines(path)
	if err != nil {
		return nil, err
	}
	if lines == nil {
		return nil, os.ErrNotExist
	}
	unescape := iniUnescape
	if multimc {
		unescape = multimcUnescape
	}
	values := map[string]string{}
	for _, line := range lines {
		if key, value, ok := splitINILine(line); ok {
			values[key] = unescape(value)
		}
	}
	return values, nil
}

func splitINILine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	key, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}

func iniEscape(value string) string {
	if value == "" {
		return value
	}
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	escaped := b.String()
	if strings.ContainsAny(value, " \t;#=\"\\") || escaped != value || strings.TrimSpace(value) != value {
		return `"` + escaped + `"`
	}
	return escaped
}

func multimcEscape(value string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "#", `\#`).Replace(value)
}

func multimcUnescape(value string) string {
	var b strings.Builder
	escape := false
	for _, r := range value {
		switch {
		case escape:
			switch r {
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			default:
				b.WriteRune(r)
			}
			escape = false
		case r == '\\':
			escape = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func iniUnescape(value string) string {
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		value = value[1 : len(value)-1]
	}
	var b strings.Builder
	escape := false
	for _, r := range value {
		switch {
		case escape:
			if r == 'n' {
				b.WriteRune('\n')
			} else {
				b.WriteRune(r)
			}
			escape = false
		case r == '\\':
			escape = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func CommandArg(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
