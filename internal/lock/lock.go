package lock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

const FileName = "shulker.lock"

type Lock struct {
	LockVersion    int             `json:"lockVersion"`
	ManifestSha256 string          `json:"manifestSha256"`
	Minecraft      string          `json:"minecraft"`
	Loader         Loader          `json:"loader"`
	Java           Java            `json:"java"`
	Packs          map[string]Pack `json:"packs"`
	Mods           map[string]Mod  `json:"mods"`
	Players        []Player        `json:"players"`
}

type Loader struct {
	Type    string     `json:"type"`
	Version string     `json:"version"`
	Server  *ServerJar `json:"server,omitempty"`
}

type ServerJar struct {
	Installer string `json:"installer"`
	Sha512    string `json:"sha512"`
}

type Java struct {
	Major     int    `json:"major"`
	Component string `json:"component"`
}

type Pack struct {
	Name      string `json:"name"`
	Ref       string `json:"ref,omitempty"`
	Commit    string `json:"commit,omitempty"`
	DirSha256 string `json:"dirSha256,omitempty"`
	Sha256    string `json:"sha256,omitempty"`
}

func (p Pack) Label() string {
	switch {
	case p.Commit != "":
		return p.Commit[:12]
	case p.DirSha256 != "":
		return p.DirSha256[:12]
	case p.Sha256 != "":
		return p.Sha256[:12]
	}
	return ""
}

type Mod struct {
	Provider      string   `json:"provider"`
	Project       any      `json:"project"`
	Version       any      `json:"version"`
	VersionNumber string   `json:"versionNumber"`
	Filename      string   `json:"filename"`
	URL           *string  `json:"url"`
	Page          string   `json:"page,omitempty"`
	Sha512        string   `json:"sha512"`
	Side          string   `json:"side"`
	RequiredBy    []string `json:"requiredBy"`
	Aliases       Aliases  `json:"aliases"`
}

type Aliases struct {
	Modrinth   string `json:"modrinth,omitempty"`
	CurseForge int    `json:"curseforge,omitempty"`
}

type Player struct {
	Name       string `json:"name"`
	UUID       string `json:"uuid"`
	ResolvedAt string `json:"resolvedAt"`
}

func New() *Lock {
	return &Lock{LockVersion: 1, Packs: map[string]Pack{}, Mods: map[string]Mod{}, Players: []Player{}}
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
		return nil, out.Errorf("lock-invalid", "%s: %v", FileName, err)
	}
	l := New()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(l); err != nil {
		return nil, out.Errorf("lock-invalid", "%s: %v", FileName, err)
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
	if l.Players == nil {
		l.Players = []Player{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(l); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (l *Lock) Save(path string) error {
	data, err := l.Encode()
	if err != nil {
		return err
	}
	if err := schema.Validate(schema.Lock, data); err != nil {
		return out.Errorf("lock-invalid", "refusing to write invalid %s: %v", FileName, err)
	}
	return os.WriteFile(path, data, 0o644)
}

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
