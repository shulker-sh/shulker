package audit

import (
	"cmp"
	"maps"
	"slices"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/security"
)

// Who decides what an entry or override folder holds: the author of a listing on a provider, the
// author of a source, or the player.
const (
	OwnerProvider = "provider"
	OwnerSource   = "source"
	OwnerPlayer   = "player"
)

// How an entry changes: only with a new pin, whenever update moves it, whenever its source moves,
// or whenever the player changes the file.
const (
	Pinned   = "pinned"
	Floating = "floating"
	Follows  = "source"
	Local    = "file"
)

// What syncs an instance before each launch.
const (
	ByHook = "hook"
	ByShim = "shim"
	ByPlay = "play"
)

// Where a launch setting comes from.
const (
	SetInInstance = "instance"
	SetInConfig   = "global"
	SetInPack     = "pack"
	SetByDefault  = "default"
)

// Control is who decides what an entry or folder holds, and how that changes.
type Control struct {
	Owner string `json:"owner"`
	// Provider and Project name the listing whose author decides: the entry's own, or the hosted
	// modpack's that brings it.
	Provider string `json:"provider,omitempty"`
	Project  string `json:"project,omitempty"`
	// Source is the source whose author decides.
	Source  string `json:"source,omitempty"`
	Changes string `json:"changes"`
	// AtLaunch marks what changes on its own when the instance launches, because a sync runs first.
	AtLaunch bool `json:"atLaunch"`
}

// Exposed is one lock entry and who controls it.
type Exposed struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Modpack string `json:"modpack,omitempty"`
	Control
}

// OverrideFolder is an override folder a build lays files from, with every feature on, and the
// files it lays.
type OverrideFolder struct {
	Folder  string `json:"folder"`
	Modpack string `json:"modpack,omitempty"`
	Control
	Files []string `json:"files"`
}

// Launch is an instance the project runs as, what syncs it before each launch, empty when nothing
// does, and the settings it launches with.
type Launch struct {
	Instance string    `json:"instance"`
	Launcher string    `json:"launcher,omitempty"`
	SyncsBy  string    `json:"syncsBy,omitempty"`
	Settings []Setting `json:"settings"`
}

// Setting is a launch setting in play and where its value comes from; an unset one has neither.
type Setting struct {
	Key   string `json:"key"`
	Value any    `json:"value,omitempty"`
	From  string `json:"from,omitempty"`
}

// Exposure is who can change what the project or instance runs.
type Exposure struct {
	Entries     []Exposed             `json:"entries"`
	Overrides   []OverrideFolder      `json:"overrides"`
	Launches    []Launch              `json:"launches"`
	Protections []security.Protection `json:"protections"`
}

// ExposureOptions are the facts an exposure map needs from outside the project.
type ExposureOptions struct {
	// Instances are the registered instances the project runs as.
	Instances []project.InstanceEntry
	// Play is config.json's play defaults.
	Play instance.LaunchSettings
	// PackMemory is the pack's client.memory.
	PackMemory  string
	Protections []security.Protection
}

// Expose maps who controls each of b's lock entries and override folders, and the launch settings
// and protections in play. It reads nothing from the network.
func Expose(b *build.Builder, o ExposureOptions) (*Exposure, error) {
	x := &Exposure{Entries: []Exposed{}, Overrides: []OverrideFolder{}, Launches: []Launch{}, Protections: o.Protections}
	c := controls{b: b}
	for _, in := range o.Instances {
		f, err := instance.Load(in.Dir)
		if err != nil {
			return nil, err
		}
		launch := Launch{Instance: in.ID, Launcher: in.Launcher, Settings: launchSettings(f.Settings.LaunchSettings, o.Play, o.PackMemory)}
		if f.Settings.PreLaunch() {
			launch.SyncsBy = syncsBy(launcher.Find(in.Launcher))
		}
		x.Launches = append(x.Launches, launch)
		c.syncs = c.syncs || launch.SyncsBy != ""
		// An instance synced from a remote source runs that source's own project, so its author
		// decides all of it. A local source is a project on this machine, the player's own.
		if !config.SameDir(b.Dir, in.Dir) && modpack.Classify(in.Source) != modpack.Local {
			c.follows = in.Source
		}
	}
	l := b.Lock
	for _, key := range slices.Sorted(maps.Keys(l.Modpacks)) {
		x.Entries = append(x.Entries, Exposed{Key: key, Type: manifest.TypeModpack, Control: c.modpack(key)})
	}
	for _, key := range slices.Sorted(maps.Keys(l.Mods)) {
		m := l.Mods[key]
		x.Entries = append(x.Entries, Exposed{Key: key, Type: manifest.TypeMod, Modpack: m.Modpack, Control: c.entry(key, m.Modpack, m.Provider, m.Project, m.File)})
	}
	for _, kind := range manifest.PackKinds {
		section := l.Packs(kind)
		for _, key := range slices.Sorted(maps.Keys(section)) {
			p := section[key]
			x.Entries = append(x.Entries, Exposed{Key: key, Type: kind, Modpack: p.Modpack, Control: c.entry(key, p.Modpack, p.Provider, p.Project, p.File)})
		}
	}
	folders := map[string]int{}
	for _, side := range b.Manifest.Sides() {
		laid, err := b.LaidFiles(side)
		if err != nil {
			return nil, err
		}
		for _, f := range laid {
			i, seen := folders[f.Folder]
			if !seen {
				i = len(x.Overrides)
				folders[f.Folder] = i
				x.Overrides = append(x.Overrides, OverrideFolder{Folder: f.Folder, Modpack: f.Modpack, Control: c.folder(f.Modpack), Files: []string{}})
			}
			if !slices.Contains(x.Overrides[i].Files, f.Path) {
				x.Overrides[i].Files = append(x.Overrides[i].Files, f.Path)
			}
		}
	}
	return x, nil
}

// InstancesOf are the registered instances b builds: the project itself in place, and those in
// the directories its sides were built into.
func InstancesOf(b *build.Builder, entries []project.InstanceEntry) []project.InstanceEntry {
	dirs := []string{b.Dir}
	for _, d := range ProjectDirs(b) {
		dirs = append(dirs, d.Path)
	}
	var found []project.InstanceEntry
	for _, e := range entries {
		if slices.ContainsFunc(dirs, func(dir string) bool { return config.SameDir(e.Dir, dir) }) {
			found = append(found, e)
		}
	}
	return found
}

func syncsBy(e *launcher.Entry) string {
	switch {
	case e == nil:
		return ""
	case e.Slot == nil:
		return ByPlay
	case e.Slot.UsesShim:
		return ByShim
	}
	return ByHook
}

// controls decides who controls what in b, where follows is the source a synced instance runs, and
// syncs whether any instance syncs before each launch.
type controls struct {
	b       *build.Builder
	follows string
	syncs   bool
}

func (c controls) entry(key, modpack, provider, project, file string) Control {
	switch {
	case c.follows != "" || modpack != "":
		return c.modpack(modpack)
	case file != "":
		return Control{Owner: OwnerPlayer, Changes: Local}
	case provider == "":
		return Control{Owner: OwnerPlayer, Changes: Pinned}
	}
	return Control{Owner: OwnerProvider, Provider: provider, Project: project, Changes: c.pinned(key)}
}

// modpack is who controls a modpack and everything it brings: a hosted one's author, who changes it
// only when update moves it, and otherwise its source's author, whose changes a sync takes when the
// modpack follows its source.
func (c controls) modpack(key string) Control {
	if c.follows != "" {
		return c.source(c.follows, true)
	}
	lp := c.b.Lock.Modpacks[key]
	if lp.Provider != "" {
		return Control{Owner: OwnerProvider, Provider: lp.Provider, Project: lp.Project, Changes: c.pinned(key)}
	}
	req, named := c.b.Manifest.Requires[key]
	return c.source(cmp.Or(lp.Source, lp.File), named && (modpack.KindOf(req) == modpack.Local || req.AutoUpdates()))
}

func (c controls) folder(modpack string) Control {
	if c.follows != "" || modpack != "" {
		return c.modpack(modpack)
	}
	return Control{Owner: OwnerPlayer, Changes: Local}
}

func (c controls) source(source string, follows bool) Control {
	if !follows {
		return Control{Owner: OwnerSource, Source: source, Changes: Floating}
	}
	return Control{Owner: OwnerSource, Source: source, Changes: Follows, AtLaunch: c.syncs}
}

func (c controls) pinned(key string) string {
	if c.b.Manifest.Requires[key].Pin != "" {
		return Pinned
	}
	return Floating
}

// launchSettings are the memory, jvmArgs and wrapper a launch runs with, each from the first place
// that sets it, in the order instance.LaunchSettings.ForLaunch takes them.
func launchSettings(own, play instance.LaunchSettings, packMemory string) []Setting {
	memory := Setting{Key: "memory", Value: instance.DefaultMemory, From: SetByDefault}
	switch {
	case own.Memory != "":
		memory.Value, memory.From = own.Memory, SetInInstance
	case play.Memory != "":
		memory.Value, memory.From = play.Memory, SetInConfig
	case packMemory != "":
		memory.Value, memory.From = packMemory, SetInPack
	}
	return []Setting{memory, listSetting("jvmArgs", own.JVMArgs, play.JVMArgs), listSetting("wrapper", own.Wrapper, play.Wrapper)}
}

// listSetting is a list the instance sets, even to nothing, over the play default.
func listSetting(key string, own, play []string) Setting {
	switch {
	case own != nil:
		return Setting{Key: key, Value: own, From: SetInInstance}
	case play != nil:
		return Setting{Key: key, Value: play, From: SetInConfig}
	}
	return Setting{Key: key}
}
