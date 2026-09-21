package game

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Version is a Minecraft version JSON as a launcher reads it: the vanilla one from Mojang, or a
// loader's own, which names the vanilla version it inherits from.
type Version struct {
	ID                 string              `json:"id"`
	InheritsFrom       string              `json:"inheritsFrom,omitempty"`
	Type               string              `json:"type,omitempty"`
	MainClass          string              `json:"mainClass,omitempty"`
	Assets             string              `json:"assets,omitempty"`
	AssetIndex         *AssetIndexRef      `json:"assetIndex,omitempty"`
	Downloads          map[string]Artifact `json:"downloads,omitempty"`
	Libraries          []Library           `json:"libraries,omitempty"`
	Arguments          *Arguments          `json:"arguments,omitempty"`
	MinecraftArguments string              `json:"minecraftArguments,omitempty"`
	JavaVersion        *JavaVersion        `json:"javaVersion,omitempty"`
	// ClientID is the version whose own downloads block holds the client jar. A loader's version
	// declares none, so it shares the jar of the vanilla version it inherits from rather than
	// filing a second copy under its own id.
	ClientID string `json:"-"`
}

// Artifact is one downloadable file. Path is set on a library's artifact, where it says where the
// file sits under the store's libraries directory.
type Artifact struct {
	Path string `json:"path,omitempty"`
	URL  string `json:"url,omitempty"`
	Sha1 string `json:"sha1,omitempty"`
	Size int64  `json:"size,omitempty"`
}

type AssetIndexRef struct {
	ID        string `json:"id"`
	URL       string `json:"url,omitempty"`
	Sha1      string `json:"sha1,omitempty"`
	Size      int64  `json:"size,omitempty"`
	TotalSize int64  `json:"totalSize,omitempty"`
}

type JavaVersion struct {
	Component string `json:"component,omitempty"`
	Major     int    `json:"majorVersion,omitempty"`
}

type Arguments struct {
	Game []Argument `json:"game,omitempty"`
	JVM  []Argument `json:"jvm,omitempty"`
}

// Argument is one entry of an argument list: either a bare string, or a value guarded by rules.
// A guarded entry's value may itself be one string or several.
type Argument struct {
	Values []string
	Rules  []Rule
}

func (a *Argument) UnmarshalJSON(data []byte) error {
	var plain string
	if err := json.Unmarshal(data, &plain); err == nil {
		*a = Argument{Values: []string{plain}}
		return nil
	}
	var guarded struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &guarded); err != nil {
		return fmt.Errorf("argument: %w", err)
	}
	a.Rules = guarded.Rules
	if err := json.Unmarshal(guarded.Value, &a.Values); err != nil {
		var one string
		if err := json.Unmarshal(guarded.Value, &one); err != nil {
			return fmt.Errorf("argument value: %w", err)
		}
		a.Values = []string{one}
	}
	return nil
}

// ParseVersion reads a version JSON, which must name its id, and starts ClientID as that id.
func ParseVersion(data []byte) (Version, error) {
	var v Version
	if err := json.Unmarshal(data, &v); err != nil {
		return Version{}, fmt.Errorf("version json: %w", err)
	}
	if v.ID == "" {
		return Version{}, fmt.Errorf("version json has no id")
	}
	v.ClientID = v.ID
	return v, nil
}

// Merge lays a version over the one it inherits from, the way the Mojang launcher does: the
// child's own fields win, its libraries go first so they shadow the parent's on the classpath, and
// its arguments go last. A child that sets minecraftArguments replaces the parent's game arguments
// outright, because a loader still using that key writes out the parent's string with its own
// additions already in it.
func Merge(parent, child Version) Version {
	merged := parent
	merged.ID = child.ID
	merged.InheritsFrom = ""
	merged.Libraries = append(append([]Library{}, child.Libraries...), parent.Libraries...)
	for _, s := range []struct{ into, from *string }{
		{&merged.Type, &child.Type},
		{&merged.MainClass, &child.MainClass},
		{&merged.Assets, &child.Assets},
	} {
		if *s.from != "" {
			*s.into = *s.from
		}
	}
	if child.AssetIndex != nil {
		merged.AssetIndex = child.AssetIndex
	}
	if child.JavaVersion != nil {
		merged.JavaVersion = child.JavaVersion
	}
	if _, ok := child.Downloads["client"]; ok {
		merged.ClientID = child.ID
	}
	if len(child.Downloads) > 0 {
		merged.Downloads = map[string]Artifact{}
		for name, d := range parent.Downloads {
			merged.Downloads[name] = d
		}
		for name, d := range child.Downloads {
			merged.Downloads[name] = d
		}
	}
	game := append([]Argument{}, parent.gameArguments()...)
	if child.MinecraftArguments != "" {
		merged.MinecraftArguments = child.MinecraftArguments
		game = splitArguments(child.MinecraftArguments)
	}
	jvm := append([]Argument{}, parent.jvmArguments()...)
	if child.Arguments != nil {
		game = append(game, child.Arguments.Game...)
		jvm = append(jvm, child.Arguments.JVM...)
	}
	merged.Arguments = &Arguments{Game: game, JVM: jvm}
	return merged
}

// gameArguments is a version's game arguments however it spells them, so that a parent using the
// legacy minecraftArguments string and a child using the arguments list can be merged as one list.
func (v Version) gameArguments() []Argument {
	if v.Arguments != nil && len(v.Arguments.Game) > 0 {
		return v.Arguments.Game
	}
	return splitArguments(v.MinecraftArguments)
}

func (v Version) jvmArguments() []Argument {
	if v.Arguments == nil {
		return nil
	}
	return v.Arguments.JVM
}

func splitArguments(s string) []Argument {
	var args []Argument
	for _, word := range strings.Fields(s) {
		args = append(args, Argument{Values: []string{word}})
	}
	return args
}
