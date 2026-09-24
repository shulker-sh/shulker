package cli

import (
	"net"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/out"
)

func parseQuickPlay(cmd *cobra.Command, world, server string) (game.QuickPlay, error) {
	worldSet, serverSet := cmd.Flags().Changed("world"), cmd.Flags().Changed("server")
	switch {
	case worldSet && serverSet:
		return game.QuickPlay{}, out.Errorf("usage", "--world and --server can't be combined: the game boots into one of them")
	case worldSet && world == "":
		return game.QuickPlay{}, out.Errorf("usage", "--world takes a save's folder name in saves/")
	case worldSet:
		return game.QuickPlay{World: world}, nil
	case !serverSet:
		return game.QuickPlay{}, nil
	}
	host, port := strings.TrimSuffix(strings.TrimPrefix(server, "["), "]"), ""
	if h, p, err := net.SplitHostPort(server); err == nil {
		host, port = h, p
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return game.QuickPlay{}, out.Errorf("usage", "--server takes an address with an optional port from 1 to 65535, and %q has %q", server, p)
		}
	}
	if host == "" {
		return game.QuickPlay{}, out.Errorf("usage", "--server takes an address, like mc.example.com or mc.example.com:25565")
	}
	return game.QuickPlay{Host: host, Port: port}, nil
}
