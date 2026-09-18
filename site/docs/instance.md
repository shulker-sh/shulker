---
description: "Every field in .shulker/instance.json, which records what an instance syncs from and how shulker sets it up, generated from its JSON Schema."
editLink: false
---

# .shulker/instance.json

One instance's own file. Hand-edit it to change what shulker does with that instance, then sync.

What an instance directory syncs from, and the settings that decide how shulker sets it up. Hand-edit it to change what shulker does, then run `shulker sync` or `shulker instances repair`. Shulker never rewrites the settings block. What the last build actually did is recorded separately, in .shulker/state.json.

Schema: [https://shulker.sh/schema/v1/instance.json](/schema/v1/instance.json)

## Properties

Required properties are marked with *.

| Property | Type | Description |
| --- | --- | --- |
| `$schema` * | `string` | Always https://shulker.sh/schema/v1/instance.json. Shulker refuses a file that names a schema it doesn't know, rather than guessing at its shape.<br>format `uri` |
| `source` * | `string` | Where this instance syncs from: a project directory, a git URL, or a manifest URL.<br>min length 1 |
| `ref` | `string` | Branch, tag, or commit to follow from a git source. Omitted follows the remote HEAD.<br>min length 1 |
| `side` * | `"client"` \| `"server"` | Which side of the source this directory is built for. A launcher instance is always the client. |
| `assumeClient` | `boolean` | Set by `--assume-client`: the source declares no client, so this directory is built from the mods and overrides both sides share. Ignored once the source declares one. |
| `unlinked` | `boolean` | Set by `shulker unlink`: shulker no longer syncs this directory, and `shulker instances repair` leaves it alone. Linking or syncing into it again clears it. |
| `settings` | object | Yours to change. Shulker seeds these when the instance is created and reads them from then on. |
| `resolved` | object | Written by shulker, for you to read. Editing it changes nothing; the next sync writes it again. |

No other properties are allowed.

## Definitions
