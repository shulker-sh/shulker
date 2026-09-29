package server

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/near"
	"shulker.sh/shulker/internal/version/minecraft"
)

// Type is the kind of value a server.properties key takes.
type Type string

const (
	String Type = ""
	Bool   Type = "bool"
	Int    Type = "int"
	Enum   Type = "enum"
	UUID   Type = "uuid"
)

type Bounds struct {
	Min int
	Max int
}

// Property is a server.properties key and the Minecraft versions that read it.
type Property struct {
	Key         string
	Type        Type
	Values      []string
	Bounds      *Bounds
	Since       string
	Until       string
	Replacement string
}

var Properties = []Property{
	{Key: "accepts-transfers", Type: Bool, Since: "1.20.5"},
	{Key: "allow-flight", Type: Bool},
	{Key: "allow-nether", Type: Bool, Until: "1.21.9", Replacement: "allowEnteringNetherUsingPortals"},
	{Key: "announce-player-achievements", Type: Bool, Until: "1.12", Replacement: "announceAdvancements"},
	{Key: "broadcast-console-to-ops", Type: Bool},
	{Key: "broadcast-rcon-to-ops", Type: Bool},
	{Key: "bug-report-link", Since: "1.21"},
	{Key: "chat-spam-threshold-seconds", Type: Int, Since: "26.2"},
	{Key: "command-spam-threshold-seconds", Type: Int, Since: "26.2"},
	{Key: "difficulty", Type: Enum, Values: []string{"peaceful", "easy", "normal", "hard", "0", "1", "2", "3"}},
	{Key: "enable-code-of-conduct", Type: Bool, Since: "1.21.9"},
	{Key: "enable-command-block", Type: Bool, Until: "1.21.9", Replacement: "enableCommandBlocks"},
	{Key: "enable-jmx-monitoring", Type: Bool, Since: "1.16"},
	{Key: "enable-query", Type: Bool},
	{Key: "enable-rcon", Type: Bool},
	{Key: "enable-status", Type: Bool, Since: "1.16"},
	{Key: "enforce-secure-profile", Type: Bool, Since: "1.19"},
	{Key: "enforce-whitelist", Type: Bool},
	{Key: "entity-broadcast-range-percentage", Type: Int, Bounds: &Bounds{10, 1000}, Since: "1.16"},
	{Key: "force-gamemode", Type: Bool},
	{Key: "function-permission-level", Type: Int, Bounds: &Bounds{1, 4}, Since: "1.14.4"},
	{Key: "gamemode", Type: Enum, Values: []string{"survival", "creative", "adventure", "spectator", "0", "1", "2", "3"}},
	{Key: "generate-structures", Type: Bool},
	{Key: "generator-settings"},
	{Key: "hardcore", Type: Bool},
	{Key: "hide-online-players", Type: Bool, Since: "1.18"},
	{Key: "initial-disabled-packs", Since: "1.19.3"},
	{Key: "initial-enabled-packs", Since: "1.19.3"},
	{Key: "level-name"},
	{Key: "level-seed"},
	{Key: "level-type"},
	{Key: "log-ips", Type: Bool, Since: "1.20.2"},
	{Key: "management-server-allowed-origins", Since: "1.21.9"},
	{Key: "management-server-enabled", Type: Bool, Since: "1.21.9"},
	{Key: "management-server-host", Since: "1.21.9"},
	{Key: "management-server-port", Type: Int, Bounds: &Bounds{0, 65535}, Since: "1.21.9"},
	{Key: "management-server-secret", Since: "1.21.9"},
	{Key: "management-server-tls-enabled", Type: Bool, Since: "1.21.9"},
	{Key: "management-server-tls-keystore", Since: "1.21.9"},
	{Key: "management-server-tls-keystore-password", Since: "1.21.9"},
	{Key: "max-build-height", Type: Int, Until: "1.17"},
	{Key: "max-chained-neighbor-updates", Type: Int, Since: "1.19"},
	{Key: "max-players", Type: Int},
	{Key: "max-tick-time", Type: Int},
	{Key: "max-world-size", Type: Int, Bounds: &Bounds{1, 29999984}},
	{Key: "motd"},
	{Key: "network-compression-threshold", Type: Int},
	{Key: "online-mode", Type: Bool},
	{Key: "op-permission-level", Type: Int, Bounds: &Bounds{0, 4}},
	{Key: "pause-when-empty-seconds", Type: Int, Since: "1.21.2"},
	{Key: "player-idle-timeout", Type: Int},
	{Key: "prevent-proxy-connections", Type: Bool},
	{Key: "previews-chat", Type: Bool, Since: "1.19", Until: "1.19.3"},
	{Key: "pvp", Type: Bool, Until: "1.21.9", Replacement: "pvp"},
	{Key: "query.port", Type: Int, Bounds: &Bounds{1, 65534}},
	{Key: "rate-limit", Type: Int, Since: "1.16.2"},
	{Key: "rcon.password"},
	{Key: "rcon.port", Type: Int, Bounds: &Bounds{1, 65534}},
	{Key: "region-file-compression", Type: Enum, Values: []string{"deflate", "lz4", "none"}, Since: "1.20.5"},
	{Key: "require-resource-pack", Type: Bool, Since: "1.17"},
	{Key: "resource-pack"},
	{Key: "resource-pack-id", Type: UUID, Since: "1.20.3"},
	{Key: "resource-pack-prompt", Since: "1.17"},
	{Key: "resource-pack-sha1"},
	{Key: "server-ip"},
	{Key: "server-port", Type: Int, Bounds: &Bounds{1, 65534}},
	{Key: "simulation-distance", Type: Int, Bounds: &Bounds{3, 32}, Since: "1.18"},
	{Key: "snooper-enabled", Type: Bool, Until: "1.18"},
	{Key: "spawn-animals", Type: Bool, Until: "1.21.2"},
	{Key: "spawn-monsters", Type: Bool, Until: "1.21.9", Replacement: "spawnMonsters"},
	{Key: "spawn-npcs", Type: Bool, Until: "1.21.2"},
	{Key: "spawn-protection", Type: Int},
	{Key: "status-heartbeat-interval", Type: Int, Since: "1.21.9"},
	{Key: "sync-chunk-writes", Type: Bool, Since: "1.16"},
	{Key: "text-filtering-config", Since: "1.16.4"},
	{Key: "text-filtering-version", Type: Int, Since: "1.21.2"},
	{Key: "use-native-transport", Type: Bool},
	{Key: "view-distance", Type: Int, Bounds: &Bounds{3, 32}},
	{Key: "white-list", Type: Bool},
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type PropertyCheck struct {
	Problems []string
	Warnings []string
}

// CheckProperties checks server.properties values against the keys and values a Minecraft version
// reads.
func CheckProperties(values map[string]string, game minecraft.Version) PropertyCheck {
	byKey := map[string]Property{}
	for _, p := range Properties {
		byKey[p.Key] = p
	}
	var c PropertyCheck
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		p, known := byKey[key]
		switch {
		case !known:
			msg := fmt.Sprintf("server.properties key %q is not a known key.", key)
			if near := nearestKey(key); near != "" {
				msg = fmt.Sprintf("server.properties key %q is not a known key; did you mean %q?", key, near)
			}
			c.Warnings = append(c.Warnings, msg)
			continue
		case p.Until != "" && minecraft.Compare(game, minecraft.MustParse(p.Until)) >= 0:
			msg := fmt.Sprintf("%s (removed in %s", key, p.Until)
			if p.Replacement != "" {
				msg += fmt.Sprintf("; use the %s game rule", p.Replacement)
			}
			c.Problems = append(c.Problems, msg+")")
			continue
		case p.Since != "" && minecraft.Compare(game, minecraft.MustParse(p.Since)) < 0:
			c.Warnings = append(c.Warnings, fmt.Sprintf("server.properties key %q was added in Minecraft %s and is ignored by %s.", key, p.Since, game))
		}
		if problem, warning := p.checkValue(values[key]); problem != "" {
			c.Problems = append(c.Problems, problem)
		} else if warning != "" {
			c.Warnings = append(c.Warnings, warning)
		}
	}
	return c
}

func (p Property) checkValue(value string) (problem, warning string) {
	if value == "" {
		return "", ""
	}
	switch p.Type {
	case Bool:
		if value != "true" && value != "false" {
			return fmt.Sprintf("%s (%q is not true or false)", p.Key, value), ""
		}
	case Int:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Sprintf("%s (%q is not an integer)", p.Key, value), ""
		}
		if b := p.Bounds; b != nil && (n < b.Min || n > b.Max) {
			return "", fmt.Sprintf("server.properties key %q is %d, outside %d-%d; the game clamps it.", p.Key, n, b.Min, b.Max)
		}
	case Enum:
		for _, v := range p.Values {
			if v == value {
				return "", ""
			}
		}
		return fmt.Sprintf("%s (%q is not one of %s)", p.Key, value, strings.Join(p.Values, ", ")), ""
	case UUID:
		if !uuidRe.MatchString(value) {
			return fmt.Sprintf("%s (%q is not a uuid)", p.Key, value), ""
		}
	}
	return "", ""
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
