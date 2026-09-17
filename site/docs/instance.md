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
| `target` * | `string` | Which of the source's targets this directory is built from. Its side is read from the target itself, never stored here.<br>min length 1 |
| `settings` | object | Yours to change. Shulker seeds these when the instance is created and reads them from then on. |
| `resolved` | object | Written by shulker, for you to read. Editing it changes nothing; the next sync writes it again. |

No other properties are allowed.

## Definitions
