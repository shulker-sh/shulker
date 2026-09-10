package server

import (
	"fmt"
	"github.com/shulker-sh/shulker/internal/near"
	"sort"

	"github.com/shulker-sh/shulker/internal/mcver"
)

type Property struct {
	Key         string
	Since       string
	Until       string
	Replacement string
}

var Properties = []Property{
	{Key: "accepts-transfers", Since: "1.20.5"},
	{Key: "allow-flight"},
	{Key: "allow-nether", Until: "1.21.9", Replacement: "allowEnteringNetherUsingPortals"},
	{Key: "announce-player-achievements", Until: "1.12", Replacement: "announceAdvancements"},
	{Key: "broadcast-console-to-ops"},
	{Key: "broadcast-rcon-to-ops"},
	{Key: "bug-report-link", Since: "1.21"},
	{Key: "chat-spam-threshold-seconds", Since: "26.2"},
	{Key: "command-spam-threshold-seconds", Since: "26.2"},
	{Key: "difficulty"},
	{Key: "enable-code-of-conduct", Since: "1.21.9"},
	{Key: "enable-command-block", Until: "1.21.9", Replacement: "enableCommandBlocks"},
	{Key: "enable-jmx-monitoring", Since: "1.16"},
	{Key: "enable-query"},
	{Key: "enable-rcon"},
	{Key: "enable-status", Since: "1.16"},
	{Key: "enforce-secure-profile", Since: "1.19"},
	{Key: "enforce-whitelist"},
	{Key: "entity-broadcast-range-percentage", Since: "1.16"},
	{Key: "force-gamemode"},
	{Key: "function-permission-level", Since: "1.14.4"},
	{Key: "gamemode"},
	{Key: "generate-structures"},
	{Key: "generator-settings"},
	{Key: "hardcore"},
	{Key: "hide-online-players", Since: "1.18"},
	{Key: "initial-disabled-packs", Since: "1.19.3"},
	{Key: "initial-enabled-packs", Since: "1.19.3"},
	{Key: "level-name"},
	{Key: "level-seed"},
	{Key: "level-type"},
	{Key: "log-ips", Since: "1.20.2"},
	{Key: "management-server-allowed-origins", Since: "1.21.9"},
	{Key: "management-server-enabled", Since: "1.21.9"},
	{Key: "management-server-host", Since: "1.21.9"},
	{Key: "management-server-port", Since: "1.21.9"},
	{Key: "management-server-secret", Since: "1.21.9"},
	{Key: "management-server-tls-enabled", Since: "1.21.9"},
	{Key: "management-server-tls-keystore", Since: "1.21.9"},
	{Key: "management-server-tls-keystore-password", Since: "1.21.9"},
	{Key: "max-build-height", Until: "1.17"},
	{Key: "max-chained-neighbor-updates", Since: "1.19"},
	{Key: "max-players"},
	{Key: "max-tick-time"},
	{Key: "max-world-size"},
	{Key: "motd"},
	{Key: "network-compression-threshold"},
	{Key: "online-mode"},
	{Key: "op-permission-level"},
	{Key: "pause-when-empty-seconds", Since: "1.21.2"},
	{Key: "player-idle-timeout"},
	{Key: "prevent-proxy-connections"},
	{Key: "previews-chat", Since: "1.19", Until: "1.19.3"},
	{Key: "pvp", Until: "1.21.9", Replacement: "pvp"},
	{Key: "query.port"},
	{Key: "rate-limit", Since: "1.16.2"},
	{Key: "rcon.password"},
	{Key: "rcon.port"},
	{Key: "region-file-compression", Since: "1.20.5"},
	{Key: "require-resource-pack", Since: "1.17"},
	{Key: "resource-pack"},
	{Key: "resource-pack-id", Since: "1.20.3"},
	{Key: "resource-pack-prompt", Since: "1.17"},
	{Key: "resource-pack-sha1"},
	{Key: "server-ip"},
	{Key: "server-port"},
	{Key: "simulation-distance", Since: "1.18"},
	{Key: "snooper-enabled", Until: "1.18"},
	{Key: "spawn-animals", Until: "1.21.2"},
	{Key: "spawn-monsters", Until: "1.21.9", Replacement: "spawnMonsters"},
	{Key: "spawn-npcs", Until: "1.21.2"},
	{Key: "spawn-protection"},
	{Key: "status-heartbeat-interval", Since: "1.21.9"},
	{Key: "sync-chunk-writes", Since: "1.16"},
	{Key: "text-filtering-config", Since: "1.16.4"},
	{Key: "text-filtering-version", Since: "1.21.2"},
	{Key: "use-native-transport"},
	{Key: "view-distance"},
	{Key: "white-list"},
}

type PropertyCheck struct {
	Problems []string
	Warnings []string
}

func CheckPropertyKeys(keys []string, minecraft mcver.Version) PropertyCheck {
	byKey := map[string]Property{}
	for _, p := range Properties {
		byKey[p.Key] = p
	}
	var c PropertyCheck
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	for _, key := range sorted {
		p, known := byKey[key]
		switch {
		case !known:
			msg := fmt.Sprintf("server.properties key %q is not a known key", key)
			if near := nearestKey(key); near != "" {
				msg += fmt.Sprintf("; did you mean %q?", near)
			}
			c.Warnings = append(c.Warnings, msg)
		case p.Until != "" && mcver.Compare(minecraft, mcver.MustParse(p.Until)) >= 0:
			msg := fmt.Sprintf("%s (removed in %s", key, p.Until)
			if p.Replacement != "" {
				msg += fmt.Sprintf("; use the %s game rule", p.Replacement)
			}
			c.Problems = append(c.Problems, msg+")")
		case p.Since != "" && mcver.Compare(minecraft, mcver.MustParse(p.Since)) < 0:
			c.Warnings = append(c.Warnings, fmt.Sprintf("server.properties key %q was added in Minecraft %s and is ignored by %s", key, p.Since, minecraft))
		}
	}
	return c
}

func nearestKey(key string) string {
	best, bestDist := "", len(key)/2+1
	for _, p := range Properties {
		if d := near.EditDistance(key, p.Key); d < bestDist || (d == bestDist && best != "" && p.Key < best) {
			best, bestDist = p.Key, d
		}
	}
	return best
}
