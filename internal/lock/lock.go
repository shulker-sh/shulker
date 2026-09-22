// Package lock reads and writes shulker.lock, the exact files a project's manifest resolved to.
package lock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"slices"
	"sort"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mcver"
	"shulker.sh/shulker/schema"
)

const FileName = "shulker.lock"

type Lock struct {
	Schema        string             `json:"$schema"`
	Minecraft     string             `json:"minecraft"`
	Loader        Loader             `json:"loader,omitzero"`
	Java          Java               `json:"java"`
	Server        *Download          `json:"server,omitempty"`
	Modpacks      map[string]Modpack `json:"modpacks"`
	Mods          map[string]Mod     `json:"mods"`
	ResourcePacks map[string]Pack    `json:"resourcepacks"`
	Shaders       map[string]Pack    `json:"shaders"`
	Datapacks     map[string]Pack    `json:"datapacks"`
	Players       []Player           `json:"players"`
}

type Loader struct {
	Type     string            `json:"type"`
	Version  string            `json:"version"`
	Provides map[string]string `json:"provides,omitempty"`
	Client   *Download         `json:"client,omitempty"`
	Server   *ServerJar        `json:"server,omitempty"`
}

type ServerJar struct {
	Installer string              `json:"installer,omitempty"`
	URL       string              `json:"url,omitempty"`
	Sha512    string              `json:"sha512"`
	Libraries map[string]Download `json:"libraries,omitempty"`
}

type Download struct {
	URL    string `json:"url"`
	Sha512 string `json:"sha512"`
}

type Java struct {
	Major     int    `json:"major"`
	Component string `json:"component"`
}

// Modpack is where a modpack resolved to: a source's commit or digest, a local archive's bytes, or
// a provider version and its archive's bytes.
type Modpack struct {
	Provider      string  `json:"provider,omitempty"`
	Project       any     `json:"project,omitempty"`
	Version       any     `json:"version,omitempty"`
	VersionNumber string  `json:"versionNumber,omitempty"`
	Channel       string  `json:"channel,omitempty"`
	URL           *string `json:"url,omitempty"`
	Page          string  `json:"page,omitempty"`
	Filename      string  `json:"filename,omitempty"`
	Source        string  `json:"source,omitempty"`
	Ref           string  `json:"ref,omitempty"`
	Commit        string  `json:"commit,omitempty"`
	DirSha256     string  `json:"dirSha256,omitempty"`
	Sha256        string  `json:"sha256,omitempty"`
	File          string  `json:"file,omitempty"`
	Sha512        string  `json:"sha512,omitempty"`
	Size          int64   `json:"size,omitempty"`
	// Unmanaged is what an archive lays as its own override files, by layer and path, each with
	// its sha512: the files no lock entry took.
	Unmanaged map[string]string `json:"unmanaged,omitempty"`
	// UsesLock marks a modpack whose mods were copied from its own lock rather than resolved from its
	// manifest.
	UsesLock   bool   `json:"locked,omitempty"`
	LockSha256 string `json:"lockSha256,omitempty"`
}

// Label names what the modpack resolved to: a hosted one's version, and a short hash otherwise.
func (p Modpack) Label() string {
	switch {
	case p.Provider != "":
		return p.VersionNumber
	case p.Commit != "":
		return p.Commit[:12]
	case p.DirSha256 != "":
		return p.DirSha256[:12]
	case p.Sha256 != "":
		return p.Sha256[:12]
	case p.Sha512 != "":
		return p.Sha512[:12]
	}
	return ""
}

// Mod is a locked mod jar. One with File is a local file: it names no provider, so every
// provider-shaped field stays empty and url is left out rather than written null.
type Mod struct {
	File          string   `json:"file,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	Project       any      `json:"project,omitempty"`
	Version       any      `json:"version,omitempty"`
	VersionNumber string   `json:"versionNumber,omitempty"`
	Filename      string   `json:"filename"`
	URL           *string  `json:"url"`
	Page          string   `json:"page,omitempty"`
	Sha512        string   `json:"sha512"`
	Size          int64    `json:"size,omitempty"`
	Side          string   `json:"side"`
	Channel       string   `json:"channel,omitempty"`
	Modpack       string   `json:"modpack,omitempty"`
	ModID         string   `json:"modId,omitempty"`
	Slug          string   `json:"slug,omitempty"`
	RequiredBy    []string `json:"requiredBy"`
	Aliases       Aliases  `json:"aliases"`
}

// Aliases are the same mod's project ids on the other providers.
type Aliases struct {
	Modrinth   string `json:"modrinth,omitempty"`
	CurseForge int    `json:"curseforge,omitempty"`
}

// Pack is a resource pack, shader or datapack: a zip placed by its requires key,
// with no jar metadata to read and nothing depending on it.
type Pack struct {
	File          string `json:"file,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Project       any    `json:"project,omitempty"`
	Version       any    `json:"version,omitempty"`
	VersionNumber string `json:"versionNumber,omitempty"`
	// Filename is where the build places the pack; ProviderFilename is the
	// provider's own name for it.
	Filename         string  `json:"filename"`
	ProviderFilename string  `json:"providerFilename,omitempty"`
	URL              *string `json:"url"`
	Page             string  `json:"page,omitempty"`
	Sha512           string  `json:"sha512"`
	Sha1             string  `json:"sha1,omitempty"`
	Size             int64   `json:"size,omitempty"`
	Channel          string  `json:"channel,omitempty"`
	Modpack          string  `json:"modpack,omitempty"`
	// Loaders are the shader mods that can load a shader; none means any can.
	Loaders []string `json:"loaders,omitempty"`
	// Side is where a datapack is placed; the other kinds are client-only.
	Side string `json:"side,omitempty"`
}

// Path is where the build places a resource pack or shader. Shaders go to shaderpacks/, except the
// vanilla ones: those are resource packs carrying core shaders, and no shader mod
// loads them.
func (p Pack) Path(kind string) string {
	if kind == manifest.TypeShader && !p.IsVanillaShader() {
		return "shaderpacks/" + p.Filename
	}
	return "resourcepacks/" + p.Filename
}

// IsVanillaShader reports a shader tagged only for vanilla. One tagged for a
// shader mod as well is a shader pack.
func (p Pack) IsVanillaShader() bool {
	return slices.Equal(p.Loaders, []string{"vanilla"})
}

func (m Mod) MarshalJSON() ([]byte, error) {
	type plain Mod
	if m.File == "" {
		return marshalPlain(plain(m))
	}
	return marshalPlain(struct {
		plain
		URL *string `json:"url,omitempty"`
	}{plain: plain(m)})
}

// MarshalJSON writes a hosted modpack's url even when it is null, as a provider entry's is.
func (p Modpack) MarshalJSON() ([]byte, error) {
	type plain Modpack
	if p.Provider == "" {
		return marshalPlain(plain(p))
	}
	return marshalPlain(struct {
		plain
		URL *string `json:"url"`
	}{plain: plain(p), URL: p.URL})
}

func (p Pack) MarshalJSON() ([]byte, error) {
	type plain Pack
	if p.File == "" {
		return marshalPlain(plain(p))
	}
	return marshalPlain(struct {
		plain
		URL *string `json:"url,omitempty"`
	}{plain: plain(p)})
}

// marshalPlain leaves & < > unescaped, as the file encoder around it does, so a url keeps its
// query string readable.
func marshalPlain(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

type Player struct {
	Name       string `json:"name"`
	UUID       string `json:"uuid"`
	ResolvedAt string `json:"resolvedAt"`
}

func New() *Lock {
	return &Lock{Schema: schema.URL(schema.Lock), Modpacks: map[string]Modpack{}, Mods: map[string]Mod{}, ResourcePacks: map[string]Pack{}, Shaders: map[string]Pack{}, Datapacks: map[string]Pack{}, Players: []Player{}}
}

// Packs is the lock's section for a pack kind, made on first use.
func (l *Lock) Packs(kind string) map[string]Pack {
	section := &l.ResourcePacks
	switch kind {
	case manifest.TypeShader:
		section = &l.Shaders
	case manifest.TypeDatapack:
		section = &l.Datapacks
	}
	if *section == nil {
		*section = map[string]Pack{}
	}
	return *section
}

// PackSections are the lock's pack sections, in manifest.PackKinds order.
func (l *Lock) PackSections() []map[string]Pack {
	sections := make([]map[string]Pack, len(manifest.PackKinds))
	for i, kind := range manifest.PackKinds {
		sections[i] = l.Packs(kind)
	}
	return sections
}

// PackPath is where a side's build places a pack of kind: a datapack in DatapackFolder, the
// other kinds by Pack.Path.
func (l *Lock) PackPath(kind string, p Pack, side, levelName string) string {
	if kind == manifest.TypeDatapack {
		folder, _ := l.DatapackFolder(side, levelName)
		return folder + "/" + p.Filename
	}
	return p.Path(kind)
}

// DatapackFolder is where a datapack placed on side goes: the folder of a global datapack mod
// placed there, else a server's world, whose datapacks vanilla loads. A client with neither gets
// the game folder's datapacks/, which only some global datapack mods read, and loaded is false.
func (l *Lock) DatapackFolder(side, levelName string) (folder string, loaded bool) {
	placed := map[string]bool{}
	for key, m := range l.Mods {
		if m.Side == "both" || m.Side == side {
			placed[l.JarID(key)] = true
		}
	}
	switch {
	case placed["paxi"]:
		return "config/paxi/datapacks", true
	case placed["openloader"]:
		if v, err := mcver.Parse(l.Minecraft); err == nil && v.Compare(mcver.MustParse("1.21")) < 0 {
			return "config/openloader/data", true
		}
		return "config/openloader/packs", true
	case side == "server":
		return levelName + "/datapacks", true
	}
	return "datapacks", false
}

// JarID is the in-jar mod id dependencies name, which is the entry's key unless
// --as gave it another one.
func (l *Lock) JarID(key string) string {
	if m, ok := l.Mods[key]; ok && m.ModID != "" {
		return m.ModID
	}
	return key
}

func Load(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Lock, error) {
	if err := schema.CheckMarker(schema.Lock, "lock-invalid", FileName, data); err != nil {
		return nil, err
	}
	if err := schema.Validate(schema.Lock, data); err != nil {
		return nil, schema.Invalid("lock-invalid", FileName, data, err)
	}
	l := New()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(l); err != nil {
		return nil, schema.Invalid("lock-invalid", FileName, data, err)
	}
	return l, nil
}

func (l *Lock) Encode() ([]byte, error) {
	l.Schema = schema.URL(schema.Lock)
	for id, m := range l.Mods {
		if m.RequiredBy == nil {
			m.RequiredBy = []string{}
		}
		sort.Strings(m.RequiredBy)
		l.Mods[id] = m
	}
	if l.ResourcePacks == nil {
		l.ResourcePacks = map[string]Pack{}
	}
	if l.Shaders == nil {
		l.Shaders = map[string]Pack{}
	}
	if l.Datapacks == nil {
		l.Datapacks = map[string]Pack{}
	}
	if l.Players == nil {
		l.Players = []Player{}
	}
	return fsutil.MarshalJSON(l)
}

func (l *Lock) Save(path string) error {
	data, err := l.encodeValid()
	if err != nil {
		return err
	}
	return fsutil.Write(path, data)
}

// Replace saves the lock over one that couldn't be read, keeping the old file as
// shulker.lock.replaced. kept is where it went, empty when there was none.
func (l *Lock) Replace(path string) (kept string, err error) {
	data, err := l.encodeValid()
	if err != nil {
		return "", err
	}
	return fsutil.Replace(path, data)
}

func (l *Lock) encodeValid() ([]byte, error) {
	data, err := l.Encode()
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(schema.Lock, data); err != nil {
		e := schema.Invalid("lock-invalid", FileName, data, err)
		e.Message = "refusing to write " + e.Message
		return nil, e
	}
	return data, nil
}

// AddRequiredBy records that by depends on the mod id, once. A mod the lock doesn't have is left
// alone.
func (l *Lock) AddRequiredBy(id, by string) {
	m, ok := l.Mods[id]
	if !ok {
		return
	}
	for _, existing := range m.RequiredBy {
		if existing == by {
			return
		}
	}
	m.RequiredBy = append(m.RequiredBy, by)
	l.Mods[id] = m
}

func FileSha256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
