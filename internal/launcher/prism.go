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
	"shulker.sh/shulker/internal/loader"
)

const (
	PrismInstanceFile = "instance.cfg"
	PrismPackFile     = "mmc-pack.json"
)

type Prism struct {
	Dir string
}

type PrismInstance struct {
	ID            string
	Name          string
	Minecraft     string
	LoaderType    string
	LoaderVersion string
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
			return "", appDataUnset()
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

func (p *Prism) Check() error {
	info, err := os.Stat(p.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w at %s", ErrNotFound, p.Dir)
	}
	return nil
}

func (p *Prism) InstancesDir() string {
	values, err := readINI(filepath.Join(p.Dir, "prismlauncher.cfg"), prismUnescape)
	if dir := values["InstanceDir"]; err == nil && dir != "" {
		if filepath.IsAbs(dir) {
			return dir
		}
		return filepath.Join(p.Dir, dir)
	}
	return filepath.Join(p.Dir, "instances")
}

func (p *Prism) WriteInstance(inst PrismInstance) (InstanceResult, error) {
	dir := filepath.Join(p.InstancesDir(), inst.ID)
	res := InstanceResult{Dir: dir}
	cfgPath := filepath.Join(dir, PrismInstanceFile)
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		res.Created = true
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	res.GameDir = prismGameDirIn(dir)
	if err := prepareGameDir(res.GameDir); err != nil {
		return res, err
	}
	if err := writePrismPack(filepath.Join(dir, PrismPackFile), inst); err != nil {
		return res, err
	}
	return res, writePrismInstanceConfig(cfgPath, inst)
}

func prismGameDirIn(dir string) string {
	gameDir := filepath.Join(dir, "minecraft")
	dotDir := filepath.Join(dir, ".minecraft")
	if _, err := os.Lstat(dotDir); err == nil {
		if _, err := os.Lstat(gameDir); errors.Is(err, os.ErrNotExist) {
			return dotDir
		}
	}
	return gameDir
}

func (p *Prism) GameDir(id string) string {
	return prismGameDirIn(filepath.Join(p.InstancesDir(), id))
}

func writePrismPack(path string, inst PrismInstance) error {
	var pack struct {
		Components    []map[string]json.RawMessage `json:"components"`
		FormatVersion int                          `json:"formatVersion"`
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &pack); err != nil {
			return invalidFile(path, err)
		}
	}
	pack.FormatVersion = 1
	l, hasLoader := loader.Lookup(inst.LoaderType)
	loaderUID := l.ComponentUID
	wanted := map[string]string{"net.minecraft": inst.Minecraft}
	if hasLoader {
		wanted[loaderUID] = inst.LoaderVersion
	}
	var components []map[string]json.RawMessage
	seen := map[string]bool{}
	for _, c := range pack.Components {
		var uid string
		_ = json.Unmarshal(c["uid"], &uid)
		if _, other := loader.ByComponentUID(uid); other && uid != loaderUID {
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
	if hasLoader && !seen[loaderUID] {
		components = append(components, map[string]json.RawMessage{"uid": jsonString(loaderUID), "version": jsonString(inst.LoaderVersion)})
	}
	pack.Components = components
	data, err := json.MarshalIndent(pack, "", "    ")
	if err != nil {
		return err
	}
	return fsutil.Write(path, append(data, '\n'))
}

// Prism reads instance.cfg with QSettings only when ConfigVersion is present;
// otherwise it uses the MultiMC-era parser, which strips backslashes but keeps
// the surrounding quotes of a QSettings-quoted value.
//
// The command slots belong to reconcile, which writes them after this, and to ReleaseSlots, which
// clears them. Touching them here would delete a command shulker hasn't had the chance to adopt.
func writePrismInstanceConfig(path string, inst PrismInstance) error {
	lines, err := readINILines(path)
	if err != nil {
		return err
	}
	set := map[string]string{"ConfigVersion": "1.3", "InstanceType": "OneSix", "name": inst.Name}
	var buf bytes.Buffer
	if len(lines) == 0 {
		lines = []string{"[General]"}
	}
	done := map[string]bool{}
	for _, line := range lines {
		key, _, ok := splitINILine(line)
		if ok {
			if key == "ConfigVersion" {
				done[key] = true
			} else if value, has := set[key]; has {
				fmt.Fprintf(&buf, "%s=%s\n", key, prismEscape(value))
				done[key] = true
				continue
			}
		}
		buf.WriteString(line + "\n")
	}
	for _, key := range []string{"ConfigVersion", "InstanceType", "name", "OverrideCommands", "PreLaunchCommand"} {
		if value, has := set[key]; has && !done[key] {
			fmt.Fprintf(&buf, "%s=%s\n", key, prismEscape(value))
		}
	}
	return fsutil.Write(path, buf.Bytes())
}

func prismEscape(value string) string {
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

func prismUnescape(value string) string {
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
