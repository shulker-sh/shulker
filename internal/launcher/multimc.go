package launcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
)

const (
	MultiMCInstanceFile = "instance.cfg"
	MultiMCPackFile     = "mmc-pack.json"
)

type MultiMC struct {
	Dir string
}

type MultiMCInstance struct {
	ID            string
	Name          string
	Minecraft     string
	LoaderType    string
	LoaderVersion string
}

func (l *MultiMC) Check() error {
	info, err := os.Stat(l.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w at %s", ErrNotFound, l.Dir)
	}
	return nil
}

func (l *MultiMC) InstancesDir() string {
	values, err := readINI(filepath.Join(l.Dir, "multimc.cfg"), multimcUnescape)
	if dir := values["InstanceDir"]; err == nil && dir != "" {
		if filepath.IsAbs(dir) {
			return dir
		}
		return filepath.Join(l.Dir, dir)
	}
	return filepath.Join(l.Dir, "instances")
}

func (l *MultiMC) WriteInstance(inst MultiMCInstance) (InstanceResult, error) {
	dir := filepath.Join(l.InstancesDir(), inst.ID)
	res := InstanceResult{Dir: dir}
	cfgPath := filepath.Join(dir, MultiMCInstanceFile)
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		res.Created = true
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	res.GameDir = multimcGameDirIn(dir)
	if err := prepareGameDir(res.GameDir); err != nil {
		return res, err
	}
	if err := writeMultiMCPack(filepath.Join(dir, MultiMCPackFile), inst); err != nil {
		return res, err
	}
	return res, writeMultiMCInstanceConfig(cfgPath, inst)
}

func multimcGameDirIn(dir string) string {
	gameDir := filepath.Join(dir, "minecraft")
	dotDir := filepath.Join(dir, ".minecraft")
	if _, err := os.Lstat(dotDir); err == nil {
		if _, err := os.Lstat(gameDir); errors.Is(err, os.ErrNotExist) {
			return dotDir
		}
	}
	return gameDir
}

func (l *MultiMC) GameDir(id string) string {
	return multimcGameDirIn(filepath.Join(l.InstancesDir(), id))
}

func writeMultiMCPack(path string, inst MultiMCInstance) error {
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

// MultiMC reads instance.cfg with its own parser only, so values are written unquoted with its
// escapes.
//
// The command slots belong to reconcile, which writes them after this, and to ReleaseSlots, which
// clears them. Touching them here would delete a command shulker hasn't had the chance to adopt.
func writeMultiMCInstanceConfig(path string, inst MultiMCInstance) error {
	lines, err := readINILines(path)
	if err != nil {
		return err
	}
	set := map[string]string{"InstanceType": "OneSix", "name": inst.Name}
	var buf bytes.Buffer
	if len(lines) == 0 {
		lines = []string{"[General]"}
	}
	done := map[string]bool{}
	for _, line := range lines {
		key, _, ok := splitINILine(line)
		if ok {
			if value, has := set[key]; has {
				fmt.Fprintf(&buf, "%s=%s\n", key, multimcEscape(value))
				done[key] = true
				continue
			}
		}
		buf.WriteString(line + "\n")
	}
	for _, key := range []string{"InstanceType", "name", "OverrideCommands", "PreLaunchCommand"} {
		if value, has := set[key]; has && !done[key] {
			fmt.Fprintf(&buf, "%s=%s\n", key, multimcEscape(value))
		}
	}
	return fsutil.Write(path, buf.Bytes())
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
