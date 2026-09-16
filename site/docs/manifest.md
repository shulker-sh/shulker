---
description: "Every field in shulker.json, the project manifest, generated from its JSON Schema."
editLink: false
---

# shulker.json

The project manifest. Hand-edited, committed, and read by every command.

Shulker project manifest. Lists direct mods, targets, and first-class configuration. Resolution results live in shulker.lock.

Schema: [https://shulker.sh/schema/v1/manifest.json](/schema/v1/manifest.json)

## Properties

Required properties are marked with *.

| Property | Type | Description |
| --- | --- | --- |
| `$schema` | `string` | format `uri` |
| `name` * | `string` | Project name. Used in messages and as the default instance name for launchers.<br>pattern `^[a-z0-9][a-z0-9._-]*$` |
| `version` | `string` | Pack version shown to people, e.g. "1.0" or "2026-09". Never parsed. Used as the versionId of an exported .mrpack and in its file name.<br>min length 1 |
| `description` | `string` | One paragraph about the pack, shown to players. First paragraph of the ModMenu entry and, joined with note, the summary of an exported .mrpack.<br>min length 1 |
| `authors` | `string`[] | Shown by ModMenu as "by ..." under the project name. init seeds the git user.name and shulker.sh; delete entries freely.<br>unique items |
| `license` | `string` | The project's license, e.g. "MIT" or "CC BY-NC-SA 4.0". Shown on the project's entry in the mod list, and clickable there when links.license is set. Defaults to "All rights reserved", since NeoForge and Forge reject a mod that declares no license.<br>min length 1 |
| `links` | [`links`](#links) |  |
| `minecraft` | [`semverRange`](#semverrange) | Semver range over Minecraft version ids, e.g. "~26.2", "^26.1", or an exact version. Pre-release order is snapshot &lt; pre &lt; rc &lt; release. Omit it to take the version from the project's locked modpacks, which then all have to agree. |
| `loader` | [`loader`](#loader) |  |
| `java` | `string` | Optional override. Either an absolute path to a JDK/JRE home or a semver range over the Java major version, e.g. "&gt;=25". Omit to derive from the Minecraft version json and use the managed runtime.<br>min length 1 |
| `providers` | [`provider`](#provider)[] | Provider preference order. A single entry makes the tool single-provider.<br>min items 1, unique items, default `["modrinth","curseforge"]` |
| `targets` * | map of [`target`](#target) | Build targets. Each lists its override layers explicitly; paths are never inferred.<br>min properties 1, keys match `^[a-z][a-z0-9_-]*$` |
| `requires` | map of [`require`](#require) | Mods and modpacks this project requires, in one map keyed by a name unique across them. A mod's key is its in-jar mod id. An entry with source is a modpack whose mods and overrides merge into this project; an empty entry is a mod at the newest release-channel file for the locked Minecraft and loader.<br>keys are [`requireKey`](#requirekey), default `{}` |
| `ignore` | [`ignore`](#ignore)[] | Per-pair overrides for unmet depends or matched breaks found in jar metadata. |
| `variables` | [`variables`](#variables) |  |
| `server` | [`server`](#server) |  |
| `client` | [`client`](#client) |  |
| `note` | [`note`](#note) |  |

No other properties are allowed.

## Definitions

### links

Links shown on the project's ModMenu entry. website, issues, and source become the Website, Issues, and Source buttons. A key ModMenu knows (discord, modrinth, curseforge, wiki, youtube, reddit, twitter, mastodon, twitch, patreon, kofi, paypal, donate, ...) uses its label; any other key is shown as written. On NeoForge and Forge, where the entry has room for less, website and issues become the Homepage and Issues buttons and license makes the license clickable.

| Property | Type | Description |
| --- | --- | --- |
| `website` | `string` | format `uri` |
| `issues` | `string` | format `uri` |
| `source` | `string` | format `uri` |
| `license` | `string` | format `uri` |
| `discord` | `string` | format `uri` |
| `modrinth` | `string` | format `uri` |
| `curseforge` | `string` | format `uri` |
| `wiki` | `string` | format `uri` |
| `youtube` | `string` | format `uri` |
| `reddit` | `string` | format `uri` |
| `twitter` | `string` | format `uri` |
| `mastodon` | `string` | format `uri` |
| `twitch` | `string` | format `uri` |
| `patreon` | `string` | format `uri` |
| `kofi` | `string` | format `uri` |
| `paypal` | `string` | format `uri` |
| `donate` | `string` | format `uri` |

### note

Free-text documentation. The comment substitute; ignored by the tool.

Type: `string`

### semverRange

Type: `string`. pattern `^\S(.*\S)?$`, min length 1

### provider

Type: `"modrinth"` \| `"curseforge"`

### osCondition

An operating system name, or !name to exclude it.

Type: `string`. pattern `^!?(macos|windows|linux)$`

### featureCondition

A feature name, or !name to require it off.

Type: `string`. pattern `^!?[A-Za-z0-9][A-Za-z0-9_.-]*$`

### featureName

Type: `string`. pattern `^[A-Za-z0-9][A-Za-z0-9_.-]*$`

### modId

In-jar mod id as declared in fabric.mod.json, quilt.mod.json or neoforge.mods.toml.

Type: `string`. pattern `^[a-z][a-z0-9_-]{1,63}$`

### projectId

Provider project id. Modrinth ids are base62 strings, CurseForge ids are integers.

Type: `string` \| `integer`

### versionId

Provider version id. Modrinth version ids are base62 strings, CurseForge file ids are integers.

Type: `string` \| `integer`

### side

Type: `"client"` \| `"server"` \| `"both"`

### loader

The mod loader. Omitted means no loader: vanilla Minecraft, which takes no mods.

| Property | Type | Description |
| --- | --- | --- |
| `type` * | `"fabric"` \| `"quilt"` \| `"neoforge"` \| `"forge"` |  |
| `version` | [`semverRange`](#semverrange) | Semver range over the loader's own version. "*", the default when omitted, selects the newest for the locked Minecraft version. |
| `note` | [`note`](#note) |  |

No other properties are allowed.

### relativePath

Path relative to the project root, forward slashes, no leading slash. The tool rejects .. segments.

Type: `string`. pattern `^[^/\\]`, min length 1

### variables

Values substituted for ${name} in *.tmpl override files and in server.properties values.

Type: map of `string` \| `number` \| `boolean`. keys match `^[A-Za-z_][A-Za-z0-9_]*$`

### target

| Property | Type | Description |
| --- | --- | --- |
| `name` | `string` | Display name launchers show for this target (Prism instance name, official launcher profile name). Defaults to the manifest name.<br>min length 1 |
| `side` * | `"client"` \| `"server"` | Which side this target builds. Mods with side "both" are included in every target. |
| `overrides` * | [`relativePath`](#relativepath)[] | Override layers applied in order; later layers win. Modpack overrides sit beneath all of these.<br>min items 1, unique items |
| `build` | [`relativePath`](#relativepath) | Output directory. Defaults to build/&lt;target&gt;. |
| `variables` | [`variables`](#variables) |  |
| `features` | [`featureName`](#featurename)[] | Features on by default for this target. A mod gated with feature ships when one of its names is on.<br>unique items |
| `wholeFiles` | [`relativePath`](#relativepath)[] | Build-relative paths or globs (* and ? match within one path segment) of .properties overrides to copy whole. Other .properties overrides merge per key: only the keys they list are managed, and keys a mod adds are left alone.<br>unique items |
| `note` | [`note`](#note) |  |

No other properties are allowed.

### requireKey

Name of a requires entry, unique across mods and modpacks. Used in messages and requiredBy.

Type: `string`. pattern `^[a-z0-9][a-z0-9._-]{0,63}$`

### require

| Property | Type | Description |
| --- | --- | --- |
| `type` | `"mod"` \| `"modpack"` \| `"resourcepack"` \| `"shader"` | What the entry is. Omitted means modpack for an entry with source and mod otherwise; when given it must agree with the entry. Resource packs and shaders aren't supported yet. |
| `source` | `string` | Local path, git URL, or raw manifest URL of a modpack.<br>min length 1 |
| `ref` | `string` | Branch, tag, or commit for a git source. The lock records the resolved commit.<br>min length 1 |
| `autoUpdate` | `boolean` | Whether sync refreshes this modpack from its source. Omitted means true; false pins the modpack at its locked state. update refreshes every modpack regardless. |
| `locked` | `boolean` | Whether the modpack's mods are copied verbatim from its lock, dependencies included, instead of resolved against this project. Omitted means true when the source has a lock; a source without a lock is always resolved from its manifest. |
| `file` | [`relativePath`](#relativepath) | A local zip or jar that is on no provider. Not supported yet. |
| `project` | [`projectId`](#projectid) | Written by add when the provider slug differs from the mod id or the provider is CurseForge. |
| `pin` | [`versionId`](#versionid) | Pin to one provider version. update skips pinned mods. |
| `channel` | `"release"` \| `"beta"` \| `"alpha"` | Least stable channel accepted. A channel admits itself and anything more stable.<br>default `"release"` |
| `side` | [`side`](#side) | Overrides the side derived from provider environment data. |
| `provider` | [`provider`](#provider) | Overrides the provider preference list for this mod. |
| `os` | [`osCondition`](#oscondition) \| [`osCondition`](#oscondition)[] | Ship only on these operating systems, evaluated where the build runs. Names are any-of, !names are none-of; both must hold. A dependency needed only by excluded mods is excluded too. |
| `feature` | [`featureCondition`](#featurecondition) \| [`featureCondition`](#featurecondition)[] | Ship only when a feature is on. Names are any-of, !names are none-of; both must hold, together with os. Features default from the target's features list. |
| `note` | [`note`](#note) |  |

No other properties are allowed.

### ignore

| Property | Type | Description |
| --- | --- | --- |
| `rule` * | `"depends"` \| `"breaks"` |  |
| `mod` * | [`modId`](#modid) | The mod whose jar metadata declares the constraint. |
| `on` * | `string` | Subject of the constraint: a mod id or one of the built-ins minecraft, java, fabricloader, quilt_loader, neoforge, forge.<br>pattern `^[a-z][a-z0-9_-]{1,63}$` |
| `declared` * | `string` | The range exactly as the jar declared it when this ignore was written. A jar that declares a different range makes the ignore stale and the original failure re-surfaces.<br>min length 1 |
| `note` * | `string` | Why this constraint is safe to ignore. Required.<br>min length 1 |

No other properties are allowed.

### player

A player by name, uuid, or both. The lock stores both after resolution.

| Property | Type | Description |
| --- | --- | --- |
| `name` | `string` | pattern `^[A-Za-z0-9_]{3,16}$` |
| `uuid` | `string` | pattern `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$` |
| `note` | [`note`](#note) |  |

### propertyValue

Type: `string` \| `integer` \| `boolean`

### serverProperties

Keys written into server.properties. Values may reference ${variables}. Known keys are listed for completion (verified against the 26.2 table on 2026-09-10); the tool validates the full set against the locked Minecraft version.

| Property | Type | Description |
| --- | --- | --- |
| `accepts-transfers` | `boolean` |  |
| `allow-flight` | `boolean` |  |
| `broadcast-console-to-ops` | `boolean` |  |
| `broadcast-rcon-to-ops` | `boolean` |  |
| `bug-report-link` | `string` |  |
| `chat-spam-threshold-seconds` | `integer` |  |
| `command-spam-threshold-seconds` | `integer` |  |
| `difficulty` | `"peaceful"` \| `"easy"` \| `"normal"` \| `"hard"` |  |
| `enable-code-of-conduct` | `boolean` |  |
| `enable-jmx-monitoring` | `boolean` |  |
| `enable-query` | `boolean` |  |
| `enable-rcon` | `boolean` |  |
| `enable-status` | `boolean` |  |
| `enforce-secure-profile` | `boolean` |  |
| `enforce-whitelist` | `boolean` |  |
| `entity-broadcast-range-percentage` | `integer` |  |
| `force-gamemode` | `boolean` |  |
| `function-permission-level` | `integer` |  |
| `gamemode` | `"survival"` \| `"creative"` \| `"adventure"` \| `"spectator"` |  |
| `generate-structures` | `boolean` |  |
| `generator-settings` | `string` |  |
| `hardcore` | `boolean` |  |
| `hide-online-players` | `boolean` |  |
| `initial-disabled-packs` | `string` |  |
| `initial-enabled-packs` | `string` |  |
| `level-name` | `string` | World folder name. Also selects which world under data/ is linked into the server build.<br>pattern `^[^/\\]+$` |
| `level-seed` | `string` \| `integer` |  |
| `level-type` | `string` |  |
| `log-ips` | `boolean` |  |
| `management-server-allowed-origins` | `string` |  |
| `management-server-enabled` | `boolean` |  |
| `management-server-host` | `string` |  |
| `management-server-port` | `integer` |  |
| `management-server-secret` | `string` |  |
| `management-server-tls-enabled` | `boolean` |  |
| `management-server-tls-keystore` | `string` |  |
| `management-server-tls-keystore-password` | `string` |  |
| `max-chained-neighbor-updates` | `integer` |  |
| `max-players` | `integer` |  |
| `max-tick-time` | `integer` |  |
| `max-world-size` | `integer` |  |
| `motd` | `string` |  |
| `network-compression-threshold` | `integer` |  |
| `online-mode` | `boolean` |  |
| `op-permission-level` | `integer` |  |
| `pause-when-empty-seconds` | `integer` |  |
| `player-idle-timeout` | `integer` |  |
| `prevent-proxy-connections` | `boolean` |  |
| `query.port` | `integer` |  |
| `rate-limit` | `integer` |  |
| `rcon.password` | `string` |  |
| `rcon.port` | `integer` |  |
| `region-file-compression` | `"deflate"` \| `"lz4"` \| `"none"` |  |
| `require-resource-pack` | `boolean` |  |
| `resource-pack` | `string` |  |
| `resource-pack-id` | `string` | pattern `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$` |
| `resource-pack-prompt` | `string` |  |
| `resource-pack-sha1` | `string` |  |
| `server-ip` | `string` |  |
| `server-port` | `integer` |  |
| `simulation-distance` | `integer` |  |
| `spawn-protection` | `integer` |  |
| `status-heartbeat-interval` | `integer` |  |
| `sync-chunk-writes` | `boolean` |  |
| `text-filtering-config` | `string` |  |
| `text-filtering-version` | `integer` |  |
| `use-native-transport` | `boolean` |  |
| `view-distance` | `integer` |  |
| `white-list` | `boolean` |  |

### server

| Property | Type | Description |
| --- | --- | --- |
| `eula` | `boolean` | eula.txt is written only when true. serve refuses to start otherwise.<br>default `false` |
| `memory` | `string` | Heap size passed as -Xms/-Xmx, e.g. "6G".<br>pattern `^[1-9][0-9]*[MmGg]$` |
| `jvmFlags` | `"aikars"` \| `"none"` | JVM flags preset used by serve. aikars applies Aikar's G1 flags (12 GB+ variant chosen from memory, -Xms set equal to -Xmx); none passes only the memory flags.<br>default `"aikars"` |
| `jvmArgs` | `string`[] | Extra JVM arguments appended after the preset, e.g. ZGC flags. |
| `properties` | [`serverProperties`](#serverproperties) |  |
| `players` | object |  |
| `note` | [`note`](#note) |  |

No other properties are allowed.

### client

| Property | Type | Description |
| --- | --- | --- |
| `options` | map of `string` \| `number` \| `boolean` | Keys written into options.txt as key:value. Other keys already in the file are left alone.<br>keys match `^[A-Za-z][A-Za-z0-9_.:]*$` |
| `servers` | object[] | Entries written into servers.dat. |
| `note` | [`note`](#note) |  |

No other properties are allowed.
