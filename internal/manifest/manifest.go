package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/shulker-sh/shulker/schema"
)

const (
	FileName  = "shulker.json"
	SchemaURL = "https://shulker.sh/schema/v1/manifest.json"
)

var DefaultProviders = []string{"modrinth", "curseforge"}

type Manifest struct {
	Schema    string            `json:"$schema,omitempty"`
	Name      string            `json:"name"`
	Version   string            `json:"version,omitempty"`
	Note      string            `json:"note,omitempty"`
	Minecraft string            `json:"minecraft"`
	Loader    Loader            `json:"loader"`
	Java      string            `json:"java,omitempty"`
	Providers []string          `json:"providers,omitempty"`
	Targets   map[string]Target `json:"targets"`
	Packs     []Pack            `json:"packs,omitempty"`
	Mods      map[string]Mod    `json:"mods"`
	Ignore    []Ignore          `json:"ignore,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
	Server    *Server           `json:"server,omitempty"`
	Client    *Client           `json:"client,omitempty"`
}

type Server struct {
	Eula       bool           `json:"eula"`
	Memory     string         `json:"memory,omitempty"`
	JvmFlags   string         `json:"jvmFlags,omitempty"`
	JvmArgs    []string       `json:"jvmArgs,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Players    *Players       `json:"players,omitempty"`
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
}

type Loader struct {
	Type    string `json:"type"`
	Version string `json:"version"`
}

type Target struct {
	Name      string            `json:"name,omitempty"`
	Side      string            `json:"side"`
	Overrides []string          `json:"overrides"`
	Build     string            `json:"build"`
	Variables map[string]string `json:"variables,omitempty"`
	Note      string            `json:"note,omitempty"`
}

type Pack struct {
	Source string `json:"source"`
	Name   string `json:"name,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Note   string `json:"note,omitempty"`
}

type Mod struct {
	Project  any    `json:"project,omitempty"`
	Pin      any    `json:"pin,omitempty"`
	Channel  string `json:"channel,omitempty"`
	Side     string `json:"side,omitempty"`
	Provider string `json:"provider,omitempty"`
	Note     string `json:"note,omitempty"`
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
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	if m.Mods == nil {
		m.Mods = map[string]Mod{}
	}
	return &m, nil
}

func (m *Manifest) Encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (m *Manifest) Save(path string) error {
	data, err := m.Encode()
	if err != nil {
		return err
	}
	if err := schema.Validate(schema.Manifest, data); err != nil {
		return fmt.Errorf("refusing to write invalid %s: %w", FileName, err)
	}
	return os.WriteFile(path, data, 0o644)
}

func (m *Manifest) DisplayName(target string) string {
	if t, ok := m.Targets[target]; ok && t.Name != "" {
		return t.Name
	}
	return m.Name
}

func (m *Manifest) ProviderOrder() []string {
	if len(m.Providers) == 0 {
		return DefaultProviders
	}
	return m.Providers
}

func (m *Manifest) ResolutionSha256() (string, error) {
	fields := struct {
		Minecraft string         `json:"minecraft"`
		Loader    Loader         `json:"loader"`
		Providers []string       `json:"providers"`
		Packs     []Pack         `json:"packs"`
		Mods      map[string]Mod `json:"mods"`
		Ignore    []Ignore       `json:"ignore"`
	}{m.Minecraft, m.Loader, m.ProviderOrder(), m.Packs, m.Mods, m.Ignore}
	data, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
