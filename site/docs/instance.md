---
description: "Every field in .shulker/instance.json, which records what an instance syncs from and how shulker sets it up, generated from its JSON Schema."
editLink: false
---

# .shulker/instance.json

One instance's own file. Hand-edit it to change what shulker does with that instance, then sync.

The settings that decide how shulker sets an instance up, and, for a directory with no shulker.json of its own, what it syncs from. Change its settings with `shulker instance set` or `shulker instance edit`, or by hand, then run `shulker sync` or `shulker instances repair`. Shulker never rewrites the settings block on its own. What the last build actually did is recorded separately, in .shulker/state.json.

Schema: [https://shulker.sh/schema/v1/instance.json](/schema/v1/instance.json)

## Properties

Required properties are marked with *.

| Property | Type | Description |
| --- | --- | --- |
| `$schema` * | `string` | Always https://shulker.sh/schema/v1/instance.json. Shulker refuses a file that names a schema it doesn't know, rather than guessing at its shape.<br>format `uri` |
| `source` | `string` | Where this instance syncs from: a project directory, a git URL, or a manifest URL. Absent in an instance that is a project of its own: the one modpack its shulker.json requires is what it follows.<br>min length 1 |
| `ref` | `string` | Branch, tag, or commit to follow from a git source. Omitted follows the remote HEAD, or, in an instance that is a project of its own, whatever its modpack entry follows.<br>min length 1 |
| `path` | `string` | Folder inside a git source's repository that holds the shulker.json to follow. Omitted means the repository root, or, in an instance that is a project of its own, whatever its modpack entry names.<br>pattern `^[^/\\]`, min length 1 |
| `side` | `"client"` \| `"server"` | Which side of the source this directory is built for. A launcher instance is always the client. Absent in an instance that is a project of its own: the side its shulker.json builds in place is the one. |
| `assumeClient` | `boolean` | Set by `--assume-client`: the source declares no client, so this directory is built from the mods and overrides both sides share. Ignored once the source declares one. |
| `unlinked` | `boolean` | Set by `shulker unlink`: shulker no longer syncs this directory, and `shulker instances repair` leaves it alone. Linking or syncing into it again clears it. |
| `settings` | object | Yours to change. Shulker seeds these when the instance is created and reads them from then on. |
| `settings.hooks` | object | Which commands the launcher runs for this instance. A hook that is off isn't installed in the launcher, and turning it off removes it on the next repair. |
| `settings.hooks.preLaunch` | `boolean` | Sync this instance from its source before each launch. Default true.<br>default `true` |
| `settings.hooks.postExit` | `boolean` | Record how each run ended when the game exits. Default true.<br>default `true` |
| `settings.commands` | object | A command shulker found in the launcher's own slot and adopted when it took the slot over, so it keeps running. The generated script runs it before anything else, and a non-zero exit from it still aborts the launch, exactly as it did before shulker was involved. `shulker unlink` puts it back in the launcher and clears this. |
| `settings.commands.preLaunch` | `string` | The pre-launch command adopted from the launcher.<br>min length 1 |
| `settings.commands.postExit` | `string` | The post-exit command adopted from the launcher.<br>min length 1 |
| `settings.marker` | `boolean` | Include the marker mod, which lists this pack in the in-game mod list and lets an export of it be recognised as this pack again. Off drops both. Set here it decides for this instance, over whatever the manifest says; absent, the manifest's marker decides, which defaults to true. Written by link --no-marker and --with-marker. |
| `settings.memory` | `string` | Heap size `shulker play` gives the game, as -Xms and -Xmx, e.g. "6G". Omitted inherits `play.memory` from config.json, then the pack's `client.memory`, and without any of them 4G. Set here, it is also written into another launcher's instance as that launcher's own memory setting, at a link, a sync or a repair; omitted, that launcher's setting is left alone, and neither `play.memory` nor the pack's reaches it.<br>pattern `^[1-9][0-9]*[MmGg]$` |
| `settings.jvmArgs` | `string`[] | Extra JVM arguments for `shulker play`, after the version's own and the memory, so one of them wins over both. Omitted inherits `play.jvmArgs` from config.json. Replaces that list rather than adding to it. Set here, it is also written into another launcher's instance as that launcher's own Java arguments; omitted, those are left alone. |
| `settings.java` | `string` | Absolute path to the Java this machine launches the instance with. Omitted inherits `play.java` from config.json when shulker launches it, and otherwise uses shulker's managed runtime. Per-machine, which is why it lives here rather than in the lock, which is shared, or the manifest, which is everyone's.<br>min length 1 |
| `settings.window` | `string` | Window size `shulker play` opens the game at, as "&lt;width&gt;x&lt;height&gt;", e.g. "1280x720". The game takes it for the run and never writes it back; fullscreen is the pack's, in `client.options`. Omitted inherits `play.window` from config.json, and `play --window` wins over both for one run. Set here, it is also written into another launcher's instance as that launcher's own window size, except ATLauncher, which keeps one size for every instance and gets a warning instead.<br>pattern `^[1-9][0-9]*x[1-9][0-9]*$` |
| `settings.wrapper` | `string`[] | Command prefix for the launch command, such as ["gamemoderun"]. For another launcher's instance, omitted leaves that launcher's own wrapper setting alone; for one shulker launches, omitted inherits `play.wrapper` from config.json. |
| `settings.sandbox` | `boolean` | Experimental. Whether the game runs sandboxed, in every launcher: it can read and write this instance and its save group, and read the Java, libraries and assets it starts from, and nothing else under your home folder or on another volume. `mods/` and `.shulker/` are read-only to it. The network stays open. Omitted inherits `security.sandbox` from config.json. On macOS and, with bubblewrap installed, Linux; elsewhere the game starts without it and shulker says why. On, shulker owns the launcher's wrapper setting: a wrapper typed into the launcher moves into `wrapper` here and still runs, outside the sandbox. On macOS the game's own "Open Folder" buttons do nothing while it is on; on Linux the game has no session bus, so a mod that needs one won't find it. |
| `settings.account` | `string` | The id of the account `shulker play` launches this instance as, over the default account. `play --account` still wins for one run. An account that has since been removed fails the launch with `account-not-found` rather than falling back to another. `shulker instance set account &lt;name&gt;` records the id of the account it names.<br>min length 1 |
| `settings.shulker` | `string` | Absolute path of the shulker binary the generated hook scripts call. Written when the scripts are, and repointed by `shulker instances repair` after the binary moves.<br>min length 1 |
| `settings.launchHistory` | `integer` | How many launch records `launches.json` keeps, newest first. -1 keeps every record; 0 keeps none and removes the file.<br>min -1, default `5` |
| `settings.savesGroup` | `string` | The save group whose worlds this instance shares: its `saves/` links to that folder under the saves root, so every instance in a group sees the same worlds. "none" keeps the worlds in the instance. Only instances shulker launches itself join a group. A change relinks on the next sync; nothing is copied or merged.<br>pattern `^[a-z0-9][a-z0-9._-]{0,63}$`, default `"default"` |
| `resolved` | object | Written by shulker, for you to read. Editing it changes nothing; the next sync writes it again. |
| `resolved.java` | `string` | The Java the last sync resolved, whether the managed runtime or the `java` setting. |
| `resolved.launcherJava` | `string` | The Java path the launcher used before shulker pointed it at its own, so `unlink` and `self uninstall` can put it back. |
| `resolved.lastSyncAt` | `string` | When this instance last synced.<br>format `date-time` |
| `resolved.lastResult` | `"ok"` \| `"failed"` | How that sync ended. A failed sync leaves the instance's files as they were, so the launcher still starts what is on disk. |

No other properties are allowed.

## Definitions
