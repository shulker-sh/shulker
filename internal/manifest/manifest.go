package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

const (
	FileName  = "shulker.json"
	SchemaURL = "https://shulker.sh/schema/v1/manifest.json"
)

var DefaultProviders = []string{"modrinth", "curseforge"}

type Manifest struct {
	Schema      string             `json:"$schema,omitempty"`
	Name        string             `json:"name"`
	Version     string             `json:"version,omitempty"`
	Description string             `json:"description,omitempty"`
	Authors     []string           `json:"authors,omitempty"`
	License     string             `json:"license,omitempty"`
	Links       map[string]string  `json:"links,omitempty"`
	Note        string             `json:"note,omitempty"`
	Minecraft   string             `json:"minecraft"`
	Loader      Loader             `json:"loader,omitzero"`
	Java        string             `json:"java,omitempty"`
	Providers   []string           `json:"providers,omitempty"`
	Targets     map[string]Target  `json:"targets"`
	Requires    map[string]Require `json:"requires"`
	Ignore      []Ignore           `json:"ignore,omitempty"`
	Variables   Variables          `json:"variables,omitempty"`
	Server      *Server            `json:"server,omitempty"`
	Client      *Client            `json:"client,omitempty"`
}

type Server struct {
	Eula       bool           `json:"eula"`
	Memory     string         `json:"memory,omitempty"`
	JvmFlags   string         `json:"jvmFlags,omitempty"`
	JvmArgs    []string       `json:"jvmArgs,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Players    *Players       `json:"players,omitempty"`
	Note       string         `json:"note,omitempty"`
}

type Players struct {
	Whitelist []Player `json:"whitelist,omitempty"`
	Ops       []Op     `json:"ops,omitempty"`
	Bans      []Ban    `json:"bans,omitempty"`
}

type Player struct {
	Name string `json:"name,omitempty"`
	UUID string `json:"uuid,omitempty"`
	Note string `json:"note,omitempty"`
}

type Op struct {
	Player
	Level               int  `json:"level,omitempty"`
	BypassesPlayerLimit bool `json:"bypassesPlayerLimit,omitempty"`
}

type Ban struct {
	Player
	Reason  string `json:"reason,omitempty"`
	Expires string `json:"expires,omitempty"`
}

func (p *Players) All() []Player {
	if p == nil {
		return nil
	}
	all := append([]Player{}, p.Whitelist...)
	for _, o := range p.Ops {
		all = append(all, o.Player)
	}
	for _, b := range p.Bans {
		all = append(all, b.Player)
	}
	return all
}

type Client struct {
	Options map[string]any  `json:"options,omitempty"`
	Servers json.RawMessage `json:"servers,omitempty"`
	Note    string          `json:"note,omitempty"`
}

type Loader struct {
	Type    string `json:"type"`
	Version string `json:"version,omitempty"`
	Note    string `json:"note,omitempty"`
}

type Target struct {
	Name       string    `json:"name,omitempty"`
	Side       string    `json:"side"`
	Overrides  []string  `json:"overrides"`
	Build      string    `json:"build,omitempty"`
	Variables  Variables `json:"variables,omitempty"`
	Features   []string  `json:"features,omitempty"`
	WholeFiles []string  `json:"wholeFiles,omitempty"`
	Note       string    `json:"note,omitempty"`
}

const (
	TypeMod          = "mod"
	TypeModpack      = "modpack"
	TypeResourcePack = "resourcepack"
	TypeShader       = "shader"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func ValidKey(key string) bool { return keyPattern.MatchString(key) }

func KeyTaken(key, held, adding string) error {
	return out.Errorf("requires-taken", "requires already has %s as a %s; pass `--as <key>` to give this %s another key", key, held, adding)
}

type Require struct {
	Type       string     `json:"type,omitempty"`
	Source     string     `json:"source,omitempty"`
	Ref        string     `json:"ref,omitempty"`
	AutoUpdate *bool      `json:"autoUpdate,omitempty"`
	Locked     *bool      `json:"locked,omitempty"`
	File       string     `json:"file,omitempty"`
	Project    any        `json:"project,omitempty"`
	Pin        any        `json:"pin,omitempty"`
	Channel    string     `json:"channel,omitempty"`
	Side       string     `json:"side,omitempty"`
	Provider   string     `json:"provider,omitempty"`
	OS         StringList `json:"os,omitempty"`
	Feature    StringList `json:"feature,omitempty"`
	Note       string     `json:"note,omitempty"`
}

func (r Require) Kind() string {
	switch {
	case r.Type != "":
		return r.Type
	case r.Source != "":
		return TypeModpack
	}
	return TypeMod
}

type StringList []string

func (l *StringList) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*l = StringList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*l = many
	return nil
}

func (l StringList) MarshalJSON() ([]byte, error) {
	if len(l) == 1 {
		return json.Marshal(l[0])
	}
	return json.Marshal([]string(l))
}

type Variables map[string]any

func (v Variables) Text() map[string]string {
	text := make(map[string]string, len(v))
	for k, value := range v {
		text[k] = fmt.Sprint(value)
	}
	return text
}

type Ignore struct {
	Rule     string `json:"rule"`
	Mod      string `json:"mod"`
	On       string `json:"on"`
	Declared string `json:"declared"`
	Note     string `json:"note"`
}

func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Manifest, error) {
	if err := schema.Validate(schema.Manifest, data); err != nil {
		return nil, schema.Invalid("manifest-invalid", FileName, data, err)
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, schema.Invalid("manifest-invalid", FileName, data, err)
	}
	if m.Requires == nil {
		m.Requires = map[string]Require{}
	}
	return &m, nil
}

func (m *Manifest) Mods() map[string]Require {
	mods := map[string]Require{}
	for key, r := range m.Requires {
		if r.Kind() == TypeMod && r.File == "" {
			mods[key] = r
		}
	}
	return mods
}

func (m *Manifest) Modpacks() map[string]Require {
	modpacks := map[string]Require{}
	for key, r := range m.Requires {
		if r.Kind() == TypeModpack && r.Source != "" {
			modpacks[key] = r
		}
	}
	return modpacks
}

func (m *Manifest) CheckSupported() error {
	for _, key := range slices.Sorted(maps.Keys(m.Requires)) {
		r := m.Requires[key]
		switch {
		case r.File != "":
			return out.Errorf("requires-unsupported", "requires.%s: local files aren't supported yet", key)
		case r.Kind() == TypeModpack && r.Source == "":
			return out.Errorf("requires-unsupported", "requires.%s: modpacks from a provider aren't supported yet; give a source", key)
		case r.Kind() != TypeMod && r.Kind() != TypeModpack:
			return out.Errorf("requires-unsupported", "requires.%s: %s entries aren't supported yet", key, r.Kind())
		}
	}
	return nil
}

func (m *Manifest) Encode() ([]byte, error) {
	return fsutil.MarshalJSON(m)
}

func (m *Manifest) Save(path string) error {
	data, err := m.Encode()
	if err != nil {
		return err
	}
	if err := schema.Validate(schema.Manifest, data); err != nil {
		e := schema.Invalid("manifest-invalid", FileName, data, err)
		e.Message = "refusing to write " + e.Message
		return e
	}
	return fsutil.Write(path, data)
}

func (m *Manifest) Target(name string) (Target, error) {
	t, ok := m.Targets[name]
	if !ok {
		e := out.Errorf("target-not-found", "no target %q in %s", name, FileName)
		e.Candidates, e.Given = slices.Sorted(maps.Keys(m.Targets)), name
		return Target{}, e
	}
	return t, nil
}

func (m *Manifest) DisplayName(target string) string {
	if t, ok := m.Targets[target]; ok && t.Name != "" {
		return t.Name
	}
	return m.Name
}

func (m *Manifest) BuildDir(target string) string {
	if t, ok := m.Targets[target]; ok && t.Build != "" {
		return t.Build
	}
	return "build/" + target
}

func (m *Manifest) ProviderOrder() []string {
	if len(m.Providers) == 0 {
		return DefaultProviders
	}
	return m.Providers
}
