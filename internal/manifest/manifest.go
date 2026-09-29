// Package manifest reads, checks and writes a project's shulker.json.
package manifest

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/managed"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

const (
	FileName  = "shulker.json"
	SchemaURL = schema.Base + string(schema.Manifest)
	// FilesDir holds the local files `add` copies into the project from outside it.
	FilesDir = "files"
)

var DefaultProviders = []string{"modrinth", "curseforge"}

// Manifest is a decoded shulker.json.
type Manifest struct {
	Schema      string             `json:"$schema,omitempty"`
	Name        string             `json:"name"`
	Version     string             `json:"version,omitempty"`
	Description string             `json:"description,omitempty"`
	Authors     []string           `json:"authors,omitempty"`
	License     string             `json:"license,omitempty"`
	Links       map[string]string  `json:"links,omitempty"`
	Icon        string             `json:"icon,omitempty"`
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
	SkipFiles   []string           `json:"skipFiles,omitempty"`
	SeedFiles   []string           `json:"seedFiles,omitempty"`
	Marker      *bool              `json:"marker,omitempty"`
	// Integrations replaces the built-in jar ids of each integration it lists, by integration id.
	Integrations map[string][]string `json:"integrations,omitempty"`
	Variables    Variables           `json:"variables,omitempty"`
	Server       *Server             `json:"server,omitempty"`
	Client       *Client             `json:"client,omitempty"`
}

type Server struct {
	Name       string         `json:"name,omitempty"`
	Build      string         `json:"build,omitempty"`
	Variables  Variables      `json:"variables,omitempty"`
	Memory     string         `json:"memory,omitempty"`
	JVMFlags   string         `json:"jvmFlags,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	// ResourcePack is the requires key of the locked resource pack whose URL and sha1 the build
	// writes into server.properties, for joining clients to download.
	ResourcePack string   `json:"resourcePack,omitempty"`
	Players      *Players `json:"players,omitempty"`
	Note         string   `json:"note,omitempty"`
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

// All lists every player the server names, whether whitelisted, opped or banned.
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
	Name      string         `json:"name,omitempty"`
	Build     string         `json:"build,omitempty"`
	Variables Variables      `json:"variables,omitempty"`
	Hooks     *Hooks         `json:"hooks,omitempty"`
	Memory    string         `json:"memory,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
	// ResourcePacks are the resource packs that start enabled, top first, and Shader the shader,
	// "" for none. Each is nil when the manifest leaves the choice to the pack and the defaults.
	ResourcePacks *[]string       `json:"resourcePacks,omitempty"`
	Shader        *string         `json:"shader,omitempty"`
	Servers       json.RawMessage `json:"servers,omitempty"`
	Note          string          `json:"note,omitempty"`
}

// BuiltinResourcePacks are the resource packs the game ships that a player may turn on, named
// as options.txt names them. The ones it always loads, such as vanilla, are never listed.
var BuiltinResourcePacks = []string{"programmer_art", "high_contrast"}

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

// Skips reports whether the override file at the build-relative path rel is left out of builds and
// exports: an operating system's folder metadata, or a match for one of SkipFiles. A pattern with no
// slash matches the file's name at any depth.
func (m *Manifest) Skips(rel string) bool {
	name := path.Base(rel)
	switch {
	case name == ".DS_Store", strings.HasPrefix(name, "._"), strings.EqualFold(name, "Thumbs.db"), strings.EqualFold(name, "desktop.ini"):
		return true
	}
	return matchesAny(m.SkipFiles, rel)
}

// Seeds reports whether the build-relative path rel is a seeded file: one of SeedFiles matches it
// the way SkipFiles would. A seeded file is the player's once written, and a build keeps their
// changes to it.
func (m *Manifest) Seeds(rel string) bool {
	return matchesAny(m.SeedFiles, rel)
}

func matchesAny(patterns []string, rel string) bool {
	for _, pattern := range patterns {
		target := rel
		if !strings.Contains(pattern, "/") {
			target = path.Base(rel)
		}
		if ok, _ := path.Match(pattern, target); ok {
			return true
		}
	}
	return false
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

// For is the folder for a side's own overrides; any side other than client gets the server's.
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
	TypeDatapack     = "datapack"
)

// PackKinds are the kinds locked as packs: zips placed under their requires key.
var PackKinds = []string{TypeResourcePack, TypeShader, TypeDatapack}

func IsPackKind(kind string) bool { return slices.Contains(PackKinds, kind) }

// TypeNouns names one and many entries of kind, for a player to read. An unknown kind is a mod.
func TypeNouns(kind string) (one, many string) {
	switch kind {
	case TypeResourcePack:
		return "resource pack", "resource packs"
	case TypeShader:
		return "shader", "shaders"
	case TypeDatapack:
		return "datapack", "datapacks"
	}
	return "mod", "mods"
}

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func IsValidKey(key string) bool { return keyPattern.MatchString(key) }

// KeyTaken is the error for adding a requires entry under a key another entry holds.
func KeyTaken(key, held, adding string) error {
	e := out.Errorf("requires-taken", "requires already has %s as a %s", key, held)
	e.Help = fmt.Sprintf("pass `--as <key>` to give this %s another key", adding)
	return e
}

// Require is one entry under requires.
type Require struct {
	Type       string `json:"type,omitempty"`
	Source     string `json:"source,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Path       string `json:"path,omitempty"`
	AutoUpdate *bool  `json:"autoUpdate,omitempty"`
	Locked     *bool  `json:"locked,omitempty"`
	File       string `json:"file,omitempty"`
	Filename   string `json:"filename,omitempty"`
	// ResourcePack also places a datapack's zip in resourcepacks/, for a hybrid carrying assets/.
	ResourcePack bool       `json:"resourcepack,omitempty"`
	Project      string     `json:"project,omitempty"`
	Pin          string     `json:"pin,omitempty"`
	Channel      string     `json:"channel,omitempty"`
	Side         string     `json:"side,omitempty"`
	Provider     string     `json:"provider,omitempty"`
	OS           StringList `json:"os,omitempty"`
	Feature      StringList `json:"feature,omitempty"`
	Note         string     `json:"note,omitempty"`
}

// ChannelOrRelease is the channel the entry accepts, release when it names none.
func (r Require) ChannelOrRelease() string {
	if r.Channel == "" {
		return "release"
	}
	return r.Channel
}

// Kind is the entry's type, where an entry with a source and no type is a modpack.
func (r Require) Kind() string {
	switch {
	case r.Type != "":
		return r.Type
	case r.Source != "":
		return TypeModpack
	}
	return TypeMod
}

// AutoUpdates reports whether sync refreshes a modpack. A hosted one never follows anything: a
// provider version's archive doesn't change, and only update moves it to another.
func (r Require) AutoUpdates() bool {
	return !r.IsHosted() && (r.AutoUpdate == nil || *r.AutoUpdate)
}

// IsHosted reports whether the entry is a modpack on a provider: one with neither a source nor a
// file.
func (r Require) IsHosted() bool {
	return r.Kind() == TypeModpack && r.Source == "" && r.File == ""
}

// StringList is a list that shulker.json may also write as a single string.
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

// Ignore silences one validation finding.
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

// Parse decodes a shulker.json and checks it against the schema and the rules the schema can't express.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := managed.Decode(schema.Manifest, FileName, data, &m); err != nil {
		return nil, sideless(data, err)
	}
	if m.Requires == nil {
		m.Requires = map[string]Require{}
	}
	if err := m.check(); err != nil {
		return nil, err
	}
	return &m, nil
}

// sideless reports a manifest that declares neither side in one sentence, in place of the schema's
// report, whose anyOf says the same thing as two missing properties.
func sideless(data []byte, err error) error {
	var declared struct {
		Client json.RawMessage `json:"client"`
		Server json.RawMessage `json:"server"`
	}
	if out.CodeOf(err) != "manifest-invalid" || json.Unmarshal(data, &declared) != nil || declared.Client != nil || declared.Server != nil {
		return err
	}
	e := out.Errorf("manifest-invalid", "%s declares no side", FileName)
	e.Rows = []out.Detail{{Label: "Fix", Text: `add "client": {} or "server": {}`}}
	return e
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
		// A pack may name a folder, so whether its file is a .zip waits for lock.
		if r := m.Requires[key]; r.File != "" && r.Kind() == TypeMod {
			if err := CheckFileExtension(key, r); err != nil {
				return err
			}
		}
		if p := m.Requires[key].Path; p != "" && !IsSubfolder(p) {
			e := out.Errorf("manifest-invalid", "requires.%s.path %q is not a folder inside the repository", key, p)
			e.Rows = []out.Detail{{Label: "Fix", Text: `give a slash-separated path relative to the repository root, such as "packs/survival"`}}
			return e
		}
		for _, name := range m.Requires[key].Feature {
			if _, ok := m.Features[strings.TrimPrefix(name, "!")]; !ok {
				e := out.Errorf("manifest-invalid", "requires.%s gates on the feature %q, which %s doesn't declare", key, strings.TrimPrefix(name, "!"), FileName)
				e.Rows = []out.Detail{{Label: "Fix", Text: `declare it under "features"`}}
				return e
			}
		}
	}
	if err := m.checkPackChoices(); err != nil {
		return err
	}
	if m.Icon != "" && !insideBuild(m.Icon) {
		e := out.Errorf("manifest-invalid", "icon %q is not a file inside the project", m.Icon)
		e.Rows = []out.Detail{{Label: "Fix", Text: `give a path relative to the project, such as "assets/icon.png"`}}
		return e
	}
	if m.BuildsInPlace("client") && m.BuildsInPlace("server") {
		e := out.Errorf("manifest-invalid", "%s builds both sides in place", FileName)
		e.Rows = []out.Detail{{Label: "Fix", Text: `give one side a "build" directory of its own`}}
		return e
	}
	return nil
}

// checkPackChoices checks that client.resourcePacks and client.shader name packs requires has, or
// a built-in resource pack. A modpack's packs aren't known until it is locked, so with a modpack in
// requires an unknown name waits for the build.
func (m *Manifest) checkPackChoices() error {
	if m.Client == nil {
		return nil
	}
	invalid := func(fix, format string, args ...any) error {
		e := out.Errorf("manifest-invalid", format, args...)
		e.Rows = []out.Detail{{Label: "Fix", Text: fix}}
		return e
	}
	hasModpack := len(m.byKind(TypeModpack)) > 0
	if list := m.Client.ResourcePacks; list != nil {
		if _, set := m.Client.Options["resourcePacks"]; set {
			return invalid("keep one of them", "client.resourcePacks and client.options.resourcePacks both set the enabled resource packs")
		}
		seen := map[string]bool{}
		for _, name := range *list {
			if seen[name] {
				return invalid("list it once", "client.resourcePacks lists %q twice", name)
			}
			seen[name] = true
			if _, ok := m.PlacedAsResourcePacks()[name]; ok || slices.Contains(BuiltinResourcePacks, name) || hasModpack {
				continue
			}
			return invalid(fmt.Sprintf("name a resource pack in requires, or %s", strings.Join(BuiltinResourcePacks, " or ")), "client.resourcePacks names %q, which is no resource pack", name)
		}
	}
	if s := m.Client.Shader; s != nil && *s != "" && !hasModpack {
		if _, ok := m.Shaders()[*s]; !ok {
			return invalid(`name a shader in requires, or "" for none`, "client.shader names %q, which is no shader", *s)
		}
	}
	return nil
}

// PlacedAsResourcePacks are the requires entries placed as resource packs: resource packs, and
// datapacks whose zip also loads as one.
func (m *Manifest) PlacedAsResourcePacks() map[string]Require {
	keys := m.ResourcePacks()
	for key, r := range m.Datapacks() {
		if r.ResourcePack {
			keys[key] = r
		}
	}
	return keys
}

// IsSubfolder reports whether rel names a folder below a repository's root, in the slash-separated
// form a git source's path takes.
func IsSubfolder(rel string) bool { return insideBuild(rel) }

func insideBuild(rel string) bool {
	if strings.Contains(rel, `\`) || path.IsAbs(rel) || (len(rel) > 1 && rel[1] == ':') {
		return false
	}
	clean := path.Clean(rel)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func (m *Manifest) Mods() map[string]Require { return m.byKind(TypeMod) }

func (m *Manifest) ResourcePacks() map[string]Require { return m.byKind(TypeResourcePack) }

func (m *Manifest) Shaders() map[string]Require { return m.byKind(TypeShader) }

func (m *Manifest) Datapacks() map[string]Require { return m.byKind(TypeDatapack) }

// Packs lists the entries of one pack kind.
func (m *Manifest) Packs(kind string) map[string]Require { return m.byKind(kind) }

// PackFilename is the name a pack is placed under: the
// entry's own filename, or its key, which stays put when the pack updates.
func PackFilename(key string, e Require) string {
	if e.Filename != "" {
		return e.Filename
	}
	return key + ".zip"
}

// IsLocalFile reports whether key is a mod or pack taken from a local file, which
// has no provider to ask for a newer version.
func (m *Manifest) IsLocalFile(key string) bool {
	r, ok := m.Requires[key]
	return ok && r.File != "" && r.Kind() != TypeModpack
}

// CheckFileExtension refuses a local file whose name doesn't end the way the lock records its kind:
// .jar for a mod, .zip for a pack.
func CheckFileExtension(key string, r Require) error {
	want := FileExtension(r.Kind())
	if strings.HasSuffix(r.File, want) {
		return nil
	}
	e := out.Errorf("manifest-invalid", "requires.%s: a %s's file must be a %s", key, r.Kind(), want)
	e.Rows = []out.Detail{{Label: "File", Text: r.File}}
	return e
}

// FileExtension is how the name of a local file of kind ends: .jar for a mod, .zip for the rest.
func FileExtension(kind string) string {
	if kind == TypeMod {
		return ".jar"
	}
	return ".zip"
}

func (m *Manifest) byKind(kind string) map[string]Require {
	found := map[string]Require{}
	for key, r := range m.Requires {
		if r.Kind() == kind {
			found[key] = r
		}
	}
	return found
}

// Modpacks lists the modpacks, from a source, an archive file or a provider.
func (m *Manifest) Modpacks() map[string]Require { return m.byKind(TypeModpack) }

func (m *Manifest) Encode() ([]byte, error) {
	return fsutil.MarshalJSON(m)
}

// Save refuses to write a manifest that fails the schema.
func (m *Manifest) Save(path string) error {
	m.Schema = SchemaURL
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

// DisplayName is the side's own name, or the project's when the side has none.
func (m *Manifest) DisplayName(side string) string {
	if b, ok := m.side(side); ok && b.Name != "" {
		return b.Name
	}
	return m.Name
}

// BuildDir is where a side builds, relative to the project; build/<side> by default.
func (m *Manifest) BuildDir(side string) string {
	if b, ok := m.side(side); ok && b.Build != "" {
		return b.Build
	}
	return "build/" + side
}

// BuildsInPlace reports whether the side builds into the project directory itself,
// which is what makes a project an instance.
func (m *Manifest) BuildsInPlace(side string) bool {
	return m.BuildDir(side) == "."
}

// InPlaceSide names the side that builds in place, which is what makes the
// project an instance that keeps history.
func (m *Manifest) InPlaceSide() (string, bool) {
	for _, side := range m.Sides() {
		if m.BuildsInPlace(side) {
			return side, true
		}
	}
	return "", false
}

// SideVariables are the top-level variables with the side's own laid over them.
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

// UsesMarker is whether a build carries the marker mod where no instance has decided for itself.
func (m *Manifest) UsesMarker() bool { return m.Marker == nil || *m.Marker }

const DefaultHistory = 5

// ClientMemory is the heap the pack's author recommends for playing it, empty where the manifest
// names none.
func (m *Manifest) ClientMemory() string {
	if m.Client == nil {
		return ""
	}
	return m.Client.Memory
}

// DefaultServerMemory is the heap a server block gets when it names none.
const DefaultServerMemory = "4G"

// HistoryKeep is how many history entries `prune` leaves and the count a build
// warns above. -1 keeps every entry, 0 takes none at all.
func (m *Manifest) HistoryKeep() int {
	if m.History == nil {
		return DefaultHistory
	}
	return *m.History
}

// ProviderOrder is the order providers are searched in, DefaultProviders when the manifest names none.
func (m *Manifest) ProviderOrder() []string {
	if len(m.Providers) == 0 {
		return DefaultProviders
	}
	return m.Providers
}
