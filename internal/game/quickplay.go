package game

import (
	"net"

	"shulker.sh/shulker/internal/out"
)

// quickPlayFloor is the first release whose version JSON declares quick play.
const quickPlayFloor = "1.20"

// QuickPlay is what a launch boots straight into instead of the title screen: a save, a server,
// or, when both are empty, neither.
type QuickPlay struct {
	World string
	Host  string
	// Port is empty when the server was named without one.
	Port string
}

// Check fails a --world launch on a Minecraft from before quick play, before anything is started.
// Launching to the title screen instead would look the same as a save that failed to load.
// --server needs no check: the pair it falls back to is older than quick play.
func (q QuickPlay) Check(v Version, minecraft string) error {
	if q.World == "" || v.Declares("is_quick_play_singleplayer") {
		return nil
	}
	return out.Errorf("unsupported-quickplay", "minecraft %s has no quick play, so --world needs %s or later", minecraft, quickPlayFloor)
}

// Apply turns the target on through the arguments the version declares for it. A server on a
// version that declares none joins through the --server and --port pair instead, as Prism does,
// which comes back to be appended.
func (q QuickPlay) Apply(v Version, features map[string]bool, vars map[string]string) (legacy []string) {
	switch {
	case q.World != "":
		features["is_quick_play_singleplayer"] = true
		vars["quickPlaySingleplayer"] = q.World
	case q.Host != "" && v.Declares("is_quick_play_multiplayer"):
		features["is_quick_play_multiplayer"] = true
		vars["quickPlayMultiplayer"] = q.Host
		if q.Port != "" {
			vars["quickPlayMultiplayer"] = net.JoinHostPort(q.Host, q.Port)
		}
	case q.Host != "":
		port := q.Port
		if port == "" {
			port = "25565"
		}
		return []string{"--server", q.Host, "--port", port}
	}
	return nil
}
