// Package lock reads and writes shulker.lock, the exact files a project's manifest resolved to.
package lock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/schema"
)

const FileName = "shulker.lock"

type Lock struct {
	LockVersion   int                `json:"lockVersion"`
	Minecraft     string             `json:"minecraft"`
	Loader        Loader             `json:"loader,omitzero"`
	Java          Java               `json:"java"`
	Server        *Download          `json:"server,omitempty"`
	Modpacks      map[string]Modpack `json:"modpacks"`
	Mods          map[string]Mod     `json:"mods"`
	ResourcePacks map[string]Pack    `json:"resourcepacks"`
	Shaders       map[string]Pack    `json:"shaders"`
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

// Modpack is where a modpack resolved to: a source's commit or digest, or a local archive's bytes.
type Modpack struct {
	Source    string `json:"source,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Commit    string `json:"commit,omitempty"`
	DirSha256 string `json:"dirSha256,omitempty"`
	Sha256    string `json:"sha256,omitempty"`
	File      string `json:"file,omitempty"`
	Sha512    string `json:"sha512,omitempty"`
	Size      int64  `json:"size,omitempty"`
	// Unmanaged is what an archive lays as its own override files, by layer and path, each with
	// its sha512: the files no lock entry took.
	Unmanaged map[string]string `json:"unmanaged,omitempty"`
	// UsesLock marks a modpack whose mods were copied from its own lock rather than resolved from its
	// manifest.
	UsesLock   bool   `json:"locked,omitempty"`
	LockSha256 string `json:"lockSha256,omitempty"`
}

// Label is the short hash that names what the modpack resolved to.
func (p Modpack) Label() string {
	switch {
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
	RequiredBy    []string `json:"requiredBy"`
	Aliases       Aliases  `json:"aliases"`
}

// Aliases are the same mod's project ids on the other providers.
type Aliases struct {
	Modrinth   string `json:"modrinth,omitempty"`
	CurseForge int    `json:"curseforge,omitempty"`
}

// Pack is a resource pack or shader: a zip placed by its requires key, with no
// jar metadata to read and nothing depending on it.
type Pack struct {
	File          string  `json:"file,omitempty"`
	Provider      string  `json:"provider,omitempty"`
	Project       any     `json:"project,omitempty"`
	Version       any     `json:"version,omitempty"`
	VersionNumber string  `json:"versionNumber,omitempty"`
	Filename      string  `json:"filename"`
	URL           *string `json:"url"`
	Page          string  `json:"page,omitempty"`
	Sha512        string  `json:"sha512"`
	Size          int64   `json:"size,omitempty"`
	Channel       string  `json:"channel,omitempty"`
	Modpack       string  `json:"modpack,omitempty"`
	// Loader is the shader loader the file targets. A vanilla shader needs no
	// shader mod and is placed and enabled as a resource pack.
	Loader string `json:"loader,omitempty"`
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
	return &Lock{LockVersion: 1, Modpacks: map[string]Modpack{}, Mods: map[string]Mod{}, ResourcePacks: map[string]Pack{}, Shaders: map[string]Pack{}, Players: []Player{}}
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
	if l.Players == nil {
		l.Players = []Player{}
	}
	return fsutil.MarshalJSON(l)
}

func (l *Lock) Save(path string) error {
	data, err := l.Encode()
	if err != nil {
		return err
	}
	if err := schema.Validate(schema.Lock, data); err != nil {
		e := schema.Invalid("lock-invalid", FileName, data, err)
		e.Message = "refusing to write " + e.Message
		return e
	}
	return fsutil.Write(path, data)
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
