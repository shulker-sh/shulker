package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

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
	Minecraft   string             `json:"minecraft,omitempty"`
	Loader      Loader             `json:"loader,omitzero"`
	Java        string             `json:"java,omitempty"`
	Providers   []string           `json:"providers,omitempty"`
	History     *int               `json:"history,omitempty"`
	Features    map[string]Feature `json:"features,omitempty"`
	Requires    map[string]Require `json:"requires"`
	Ignore      []Ignore           `json:"ignore,omitempty"`
	WholeFiles  []string           `json:"wholeFiles,omitempty"`
	Marker      *bool              `json:"marker,omitempty"`
	Variables   Variables          `json:"variables,omitempty"`
	Server      *Server            `json:"server,omitempty"`
	Client      *Client            `json:"client,omitempty"`
}

type Server struct {
	Name       string         `json:"name,omitempty"`
	Build      string         `json:"build,omitempty"`
	Variables  Variables      `json:"variables,omitempty"`
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
	Name      string          `json:"name,omitempty"`
	Build     string          `json:"build,omitempty"`
	Variables Variables       `json:"variables,omitempty"`
	Hooks     *Hooks          `json:"hooks,omitempty"`
	Options   map[string]any  `json:"options,omitempty"`
	Servers   json.RawMessage `json:"servers,omitempty"`
	Note      string          `json:"note,omitempty"`
}

// Hooks are the author's defaults for a launcher instance's hook switches. They seed the instance's
// own settings when it is created; nothing reads them after that.
type Hooks struct {
	PreLaunch *bool `json:"preLaunch,omitempty"`
	PostExit  *bool `json:"postExit,omitempty"`
}

type Loader struct {
	Type    string `json:"type"`
	Version string `json:"version,omitempty"`
	Note    string `json:"note,omitempty"`
}

type Feature struct {
	Default   bool             `json:"default,omitempty"`
	Note      string           `json:"note,omitempty"`
	Overrides FeatureOverrides `json:"overrides,omitzero"`
}

// FeatureOverrides is where a feature's override files live: one folder for
// both sides, or a folder per side. Both empty means the default folder.
type FeatureOverrides struct {
	Both   string
	Client string
	Server string
}

func (o *FeatureOverrides) UnmarshalJSON(data []byte) error {
	var both string
	if err := json.Unmarshal(data, &both); err == nil {
		*o = FeatureOverrides{Both: both}
		return nil
	}
	var sides struct {
		Client string `json:"client"`
		Server string `json:"server"`
	}
	if err := json.Unmarshal(data, &sides); err != nil {
		return err
	}
	*o = FeatureOverrides{Client: sides.Client, Server: sides.Server}
	return nil
}

func (o FeatureOverrides) For(side string) string {
	if side == "client" {
		return o.Client
	}
	return o.Server
}

func (o FeatureOverrides) MarshalJSON() ([]byte, error) {
	if o.Both != "" {
		return json.Marshal(o.Both)
	}
	sides := map[string]string{}
	if o.Client != "" {
		sides["client"] = o.Client
	}
	if o.Server != "" {
		sides["server"] = o.Server
	}
	return json.Marshal(sides)
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

func (r Require) AutoUpdates() bool { return r.AutoUpdate == nil || *r.AutoUpdate }

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
	// Before the schema, whose anyOf reports the same thing as two missing
	// properties.
	var declared struct {
		Client json.RawMessage `json:"client"`
		Server json.RawMessage `json:"server"`
	}
	if json.Unmarshal(data, &declared) == nil && declared.Client == nil && declared.Server == nil {
		e := out.Errorf("manifest-invalid", "%s declares no side", FileName)
		e.Rows = []out.Detail{{Label: "Fix", Text: `add "client": {} or "server": {}`}}
		return nil, e
	}
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
	if err := m.check(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) check() error {
	for _, name := range slices.Sorted(maps.Keys(m.Features)) {
		if name == "client" || name == "server" {
			e := out.Errorf("manifest-invalid", "%s names a feature %q, which is the side's own name", FileName, name)
			e.Rows = []out.Detail{{Label: "Fix", Text: "rename the feature"}}
			return e
		}
	}
	for _, key := range slices.Sorted(maps.Keys(m.Requires)) {
		for _, name := range m.Requires[key].Feature {
			if _, ok := m.Features[strings.TrimPrefix(name, "!")]; !ok {
				e := out.Errorf("manifest-invalid", "requires.%s gates on the feature %q, which %s doesn't declare", key, strings.TrimPrefix(name, "!"), FileName)
				e.Rows = []out.Detail{{Label: "Fix", Text: `declare it under "features"`}}
				return e
			}
		}
	}
	if m.InPlace("client") && m.InPlace("server") {
		e := out.Errorf("manifest-invalid", "%s builds both sides in place", FileName)
		e.Rows = []out.Detail{{Label: "Fix", Text: `give one side a "build" directory of its own`}}
		return e
	}
	return nil
}

func (m *Manifest) Mods() map[string]Require { return m.byKind(TypeMod) }

func (m *Manifest) ResourcePacks() map[string]Require { return m.byKind(TypeResourcePack) }

func (m *Manifest) Shaders() map[string]Require { return m.byKind(TypeShader) }

func (m *Manifest) byKind(kind string) map[string]Require {
	found := map[string]Require{}
	for key, r := range m.Requires {
		if r.Kind() == kind && r.File == "" {
			found[key] = r
		}
	}
	return found
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

var SideNames = []string{"client", "server"}

func IsSide(name string) bool { return name == "client" || name == "server" }

func NotASide(name string) *out.Error {
	e := out.Errorf("usage", "%q is not a side", name)
	e.Candidates, e.Given = SideNames, name
	return e
}

type sideBlock struct {
	Name      string
	Build     string
	Variables Variables
}

func (m *Manifest) side(side string) (sideBlock, bool) {
	switch {
	case side == "client" && m.Client != nil:
		return sideBlock{m.Client.Name, m.Client.Build, m.Client.Variables}, true
	case side == "server" && m.Server != nil:
		return sideBlock{m.Server.Name, m.Server.Build, m.Server.Variables}, true
	}
	return sideBlock{}, false
}

func (m *Manifest) Sides() []string {
	var sides []string
	for _, side := range SideNames {
		if m.HasSide(side) {
			sides = append(sides, side)
		}
	}
	return sides
}

func (m *Manifest) HasSide(side string) bool {
	_, ok := m.side(side)
	return ok
}

func (m *Manifest) DisplayName(side string) string {
	if b, ok := m.side(side); ok && b.Name != "" {
		return b.Name
	}
	return m.Name
}

func (m *Manifest) BuildDir(side string) string {
	if b, ok := m.side(side); ok && b.Build != "" {
		return b.Build
	}
	return "build/" + side
}

// InPlace reports whether the side builds into the project directory itself,
// which is what makes a project an instance.
func (m *Manifest) InPlace(side string) bool {
	return m.BuildDir(side) == "."
}

// InPlaceSide names the side that builds in place, which is what makes the
// project an instance that keeps history.
func (m *Manifest) InPlaceSide() (string, bool) {
	for _, side := range m.Sides() {
		if m.InPlace(side) {
			return side, true
		}
	}
	return "", false
}

func (m *Manifest) SideVariables(side string) Variables {
	vars := Variables{}
	maps.Copy(vars, m.Variables)
	if b, ok := m.side(side); ok {
		maps.Copy(vars, b.Variables)
	}
	return vars
}

// ClientHooks are the author's hook defaults for a new instance, absent where the manifest says
// nothing and the instance's own default stands.
func (m *Manifest) ClientHooks() Hooks {
	if m.Client == nil || m.Client.Hooks == nil {
		return Hooks{}
	}
	return *m.Client.Hooks
}

// MarkerOn is whether a build carries the marker mod where no instance has decided for itself.
func (m *Manifest) MarkerOn() bool { return m.Marker == nil || *m.Marker }

const DefaultHistory = 5

// HistoryKeep is how many history entries `prune` leaves and the count a build
// warns above. -1 keeps every entry, 0 takes none at all.
func (m *Manifest) HistoryKeep() int {
	if m.History == nil {
		return DefaultHistory
	}
	return *m.History
}

func (m *Manifest) ProviderOrder() []string {
	if len(m.Providers) == 0 {
		return DefaultProviders
	}
	return m.Providers
}
