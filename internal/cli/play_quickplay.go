package cli

import (
	"net"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/out"
)

// quickPlayFloor is the first release whose version JSON declares quick play.
const quickPlayFloor = "1.20"

// quickPlay is what a launch boots straight into instead of the title screen: a save, a server,
// or, when both are empty, neither.
type quickPlay struct {
	world string
	host  string
	// port is empty when the server was named without one.
	port string
}

func parseQuickPlay(cmd *cobra.Command, world, server string) (quickPlay, error) {
	worldSet, serverSet := cmd.Flags().Changed("world"), cmd.Flags().Changed("server")
	switch {
	case worldSet && serverSet:
		return quickPlay{}, out.Errorf("usage", "--world and --server can't be combined: the game boots into one of them")
	case worldSet && world == "":
		return quickPlay{}, out.Errorf("usage", "--world takes a save's folder name in saves/")
	case worldSet:
		return quickPlay{world: world}, nil
	case !serverSet:
		return quickPlay{}, nil
	}
	host, port := strings.TrimSuffix(strings.TrimPrefix(server, "["), "]"), ""
	if h, p, err := net.SplitHostPort(server); err == nil {
		host, port = h, p
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return quickPlay{}, out.Errorf("usage", "--server takes an address with an optional port from 1 to 65535, and %q has %q", server, p)
		}
	}
	if host == "" {
		return quickPlay{}, out.Errorf("usage", "--server takes an address, like mc.example.com or mc.example.com:25565")
	}
	return quickPlay{host: host, port: port}, nil
}

// checkQuickPlay fails a --world launch on a Minecraft from before quick play, before anything is
// started. Launching to the title screen instead would look the same as a save that failed to load.
// --server needs no check: the pair it falls back to is older than quick play.
func checkQuickPlay(v game.Version, minecraft string, q quickPlay) error {
	if q.world == "" || v.Declares("is_quick_play_singleplayer") {
		return nil
	}
	return out.Errorf("unsupported-quickplay", "minecraft %s has no quick play, so --world needs %s or later", minecraft, quickPlayFloor)
}

// apply turns the target on through the arguments the version declares for it. A server on a
// version that declares none joins through the --server and --port pair instead, as Prism does,
// which comes back to be appended.
func (q quickPlay) apply(v game.Version, features map[string]bool, vars map[string]string) (legacy []string) {
	switch {
	case q.world != "":
		features["is_quick_play_singleplayer"] = true
		vars["quickPlaySingleplayer"] = q.world
	case q.host != "" && v.Declares("is_quick_play_multiplayer"):
		features["is_quick_play_multiplayer"] = true
		vars["quickPlayMultiplayer"] = q.host
		if q.port != "" {
			vars["quickPlayMultiplayer"] = net.JoinHostPort(q.host, q.port)
		}
	case q.host != "":
		port := q.port
		if port == "" {
			port = "25565"
		}
		return []string{"--server", q.host, "--port", port}
	}
	return nil
}
