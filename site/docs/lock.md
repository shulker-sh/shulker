---
description: "Every field in shulker.lock, which records exact mod versions and hashes, generated from its JSON Schema."
editLink: false
---

# shulker.lock

The lock file. Written by the CLI, committed alongside the manifest, never hand-edited.

Shulker lock file. Tool-owned, key-sorted, pretty-printed. Records the exact resolution of shulker.json; install and build read only this file.

Schema: [https://shulker.sh/schema/v1/lock.json](/schema/v1/lock.json)

## Properties

Required properties are marked with *.

| Property | Type | Description |
| --- | --- | --- |
| `lockVersion` * | `1` |  |
| `minecraft` * | `string` | Resolved Minecraft version id, exactly as Mojang's manifest names it.<br>min length 1 |
| `loader` * | object |  |
| `java` * | object |  |
| `packs` * | map of [`pack`](#pack) | Keyed by manifest source string. |
| `mods` * | map of [`mod`](#mod) | Every mod in the resolved set, direct and transitive, keyed by in-jar mod id.<br>keys are [`modId`](#modid) |
| `players` * | [`player`](#player)[] | Every player referenced anywhere in the manifest, fully resolved. uuid is the identity across renames. |

No other properties are allowed.

## Definitions

### sha256

Type: `string`. pattern `^[0-9a-f]{64}$`

### sha512

Type: `string`. pattern `^[0-9a-f]{128}$`

### download

| Property | Type | Description |
| --- | --- | --- |
| `url` * | `string` | format `uri` |
| `sha512` * | [`sha512`](#sha512) |  |

No other properties are allowed.

### gitCommit

Type: `string`. pattern `^[0-9a-f]{40}$`

### uuid

Type: `string`. pattern `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`

### modId

Type: `string`. pattern `^[a-z][a-z0-9_-]{1,63}$`

### packName

Type: `string`. pattern `^[a-z0-9][a-z0-9._-]*$`

### modrinthId

Type: `string`. pattern `^[A-Za-z0-9]{8}$`

### curseforgeId

Type: `integer`. min 1

### pack

| Property | Type | Description |
| --- | --- | --- |
| `name` * | [`packName`](#packname) | Manifest name or the derived default. |
| `ref` | `string` | The manifest ref at resolution time. Git sources only.<br>min length 1 |
| `commit` | [`gitCommit`](#gitcommit) | Resolved commit. Git sources only. |
| `dirSha256` | [`sha256`](#sha256) | Content hash of the pack's shulker.json and override directories in sorted path order, excluding its lock, build output, and data. Local-path sources only. |
| `sha256` | [`sha256`](#sha256) | Hash of the fetched manifest. Raw manifest URL sources only. |

No other properties are allowed.

### mod

| Property | Type | Description |
| --- | --- | --- |
| `provider` * | `"modrinth"` \| `"curseforge"` |  |
| `project` * | [`modrinthId`](#modrinthid) \| [`curseforgeId`](#curseforgeid) | Provider project id, typed as the provider types it. |
| `version` * | [`modrinthId`](#modrinthid) \| [`curseforgeId`](#curseforgeid) | Provider version id (Modrinth) or file id (CurseForge). |
| `versionNumber` * | `string` | Provider display string. Never parsed.<br>min length 1 |
| `filename` * | `string` | pattern `^[^/\\]+\.jar$`, min length 1 |
| `url` * | `string` \| `null` | Download url. null when the author disabled third-party distribution; page is then required.<br>format `uri` |
| `page` | `string` | Provider file page for manual download. Present only when url is null.<br>format `uri` |
| `sha512` * | [`sha512`](#sha512) | Cache key and install verification hash. |
| `size` | `integer` | Jar size in bytes as the provider reports it, for the download bar. Absent on entries locked before shulker recorded sizes; filled in when the mod is next resolved.<br>min 1 |
| `side` * | `"client"` \| `"server"` \| `"both"` | Effective side after any manifest override. |
| `channel` * | `"release"` \| `"beta"` \| `"alpha"` | Least stable channel accepted when this version was picked: the manifest's channel for a mod listed there or in a pack, the requiring mod's for a dependency. A listed mod whose manifest channel differs makes the lock out of date. |
| `requiredBy` * | `string`[] | Mod ids (or pack names) whose jar metadata depends on this mod. Empty plus absent from manifest and packs means orphan.<br>unique items |
| `aliases` * | object | The same mod's project id on other providers, discovered on first download. |

No other properties are allowed.

### player

| Property | Type | Description |
| --- | --- | --- |
| `name` * | `string` | pattern `^[A-Za-z0-9_]{3,16}$` |
| `uuid` * | [`uuid`](#uuid) |  |
| `resolvedAt` * | `string` | When this name/uuid pair was last confirmed against Mojang.<br>format `date-time` |

No other properties are allowed.
