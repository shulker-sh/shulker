---
description: Every shulker command with its flags and examples.
outline: [2, 3]
---

# CLI reference

| Command | Description |
| --- | --- |
| [`shulker init`](#shulker-init) | Create shulker.json and a lock in the current directory |
| [`shulker add <mod>...`](#shulker-add) | Add mods to the manifest and lock |
| [`shulker remove <mod>...`](#shulker-remove) | Remove mods from the manifest and lock |
| [`shulker lock`](#shulker-lock) | Bring the lock in line with shulker.json without upgrading |
| [`shulker update [mod...]`](#shulker-update) | Update mods to the newest compatible version |
| [`shulker outdated [mod...]`](#shulker-outdated) | Show mods with a newer compatible version |
| [`shulker suggests`](#shulker-suggests) | List mods that locked mods recommend and that aren't installed |
| [`shulker pin <mod> [version]`](#shulker-pin) | Pin a mod to a provider version id |
| [`shulker unpin <mod>`](#shulker-unpin) | Remove a mod's pin and re-resolve it |
| [`shulker target add <name>`](#shulker-target-add) | Add a build target |
| [`shulker target remove <name>`](#shulker-target-remove) | Remove a target, leaving its build directory |
| [`shulker target list`](#shulker-target-list) | List targets |
| [`shulker set <path> <value>`](#shulker-set) | Set a field in shulker.json |
| [`shulker unset <path>`](#shulker-unset) | Remove a field from shulker.json |
| [`shulker get [path]`](#shulker-get) | Print a field of shulker.json, or all of it |
| [`shulker feature on\|off <feature>`](#shulker-feature-on-off) | Turn a feature on or off on this machine |
| [`shulker feature reset <feature>`](#shulker-feature-reset) | Go back to the target defaults for a feature |
| [`shulker feature list`](#shulker-feature-list) | List features and whether they're on |
| [`shulker install`](#shulker-install) | Download everything in the lock and build all targets |
| [`shulker build [target]`](#shulker-build) | Assemble build directories from the lock and overrides |
| [`shulker diff [target]`](#shulker-diff) | Show build files that differ from what build would write |
| [`shulker pull [file...]`](#shulker-pull) | Copy edits made in a build directory back into their source |
| [`shulker serve [target]`](#shulker-serve) | Build a server target and run it in the foreground |
| [`shulker link mojang`](#shulker-link-mojang) | Add a profile for the client build to the official launcher |
| [`shulker link prism`](#shulker-link-prism) | Create a Prism Launcher or MultiMC instance for the client build |
| [`shulker sync [source]`](#shulker-sync) | Download and build one target of a project into a directory, or update a linked one |
| [`shulker links`](#shulker-links) | List linked launcher instances and synced directories |
| [`shulker unlink <name>`](#shulker-unlink) | Stop syncing a linked instance or synced directory, keeping its files |
| [`shulker pack add <source>`](#shulker-pack-add) | Add a pack from a local path, git URL, or manifest URL |
| [`shulker pack remove <name>`](#shulker-pack-remove) | Remove a pack |
| [`shulker pack list`](#shulker-pack-list) | List packs and their local drift state |
| [`shulker player [name\|uuid]...`](#shulker-player) | Check player names and uuids against Mojang and the lock |
| [`shulker import mrpack <file>`](#shulker-import-mrpack) | Create a project from a Modrinth modpack |
| [`shulker export mrpack`](#shulker-export-mrpack) | Export a Modrinth modpack |
| [`shulker version`](#shulker-version) | Print the shulker version |
| [`shulker self update`](#shulker-self-update) | Update shulker to the latest release |

## Global flags

These work with every command.

| Flag | Description |
| --- | --- |
| `-C, --dir <path>` | Project directory (default: current directory) |
| `--json` | Print machine-readable JSON, including errors; see [JSON output](#json-output) |

## Projects

### `shulker init`

Create `shulker.json` and `shulker.lock` in the current directory. Pass `--yes` for the defaults, or set at least `--name` and `--minecraft`.

```sh
shulker init --yes
shulker init --name my-server --minecraft 1.21.1 --loader neoforge --target server
```

| Flag | Description |
| --- | --- |
| `-y, --yes` | Accept defaults: latest release, fabric, client target |
| `--name <name>` | Project name (default: directory name) |
| `--minecraft <version>` | Minecraft version or range (default: latest release) |
| `--loader <loader>` | Mod loader: `fabric`, `quilt`, `neoforge`, `forge` |
| `--loader-version <range>` | Loader version range (default: `*`) |
| `--target <side>` | First target: `client` or `server` |

### `shulker import mrpack`

Create a project from a Modrinth modpack (`.mrpack`).

```sh
shulker import mrpack ~/Downloads/fabulously-optimized.mrpack
shulker import mrpack pack.mrpack -C my-pack --name my-pack
```

| Flag | Description |
| --- | --- |
| `--name <name>` | Project name (default: the pack name, slugified) |

### `shulker export mrpack`

Export the project as a Modrinth modpack for the Modrinth app and other launchers.

```sh
shulker export mrpack
shulker export mrpack --target client -o dist/my-pack.mrpack
```

| Flag | Description |
| --- | --- |
| `--version <version>` | Version id written into the pack (default: `version` in shulker.json) |
| `-o, --output <path>` | Archive path (default: `build/<name>-<version>.mrpack`) |
| `--target <name>` | Export one target only (default: every target) |
| `--os <os>` | Include mods gated on this OS: `macos`, `windows`, or `linux` (default: leave them out) |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |
| `--bundle` | Put mods that Modrinth launchers can't download inside the archive |

## Mods

### `shulker add`

Add mods to the manifest, resolve them and their dependencies, and write the lock. Mods are named by their provider slug.

```sh
shulker add sodium lithium
shulker add iris --channel beta
shulker add betterthirdperson --provider curseforge --side client
```

| Flag | Description |
| --- | --- |
| `--side <side>` | Override side: `client`, `server`, `both` |
| `--channel <channel>` | Least stable channel accepted: `release`, `beta`, `alpha` |
| `--pin <version-id>` | Pin to a provider version id (one mod only) |
| `--provider <provider>` | Provider to use for this mod: `modrinth` or `curseforge` |

### `shulker remove`

Remove mods from the manifest and prune dependencies nothing else needs. Alias: `rm`.

```sh
shulker remove lithium
```

### `shulker lock`

Bring `shulker.lock` in line with `shulker.json` after you edit it by hand, without upgrading anything. Mods new to `shulker.json` are resolved, mods nothing lists or requires are dropped, and a mod whose channel, pin, side, provider, or project changed is picked again. Every other mod keeps its locked version, and packs stay at their locked commit unless their `ref` changed. When the locked Minecraft or loader version no longer matches `shulker.json`, or a mod is locked from a provider `shulker.json` no longer lists, every mod is resolved again and `reresolved` says why. Without a `shulker.lock`, `lock` creates one.

`add`, `remove`, `update`, `pin`, `unpin`, `pack add`, and `pack remove` do the same before their own change, so a hand edit is never left out of the lock. What they bring in shows up in their output.

```sh
shulker lock
```

### `shulker update`

Re-resolve mods to the newest compatible versions. With no arguments, updates every mod. Alias: `upgrade`.

```sh
shulker update
shulker update sodium iris
```

### `shulker outdated`

Show mods with a newer compatible version without changing anything, like a dry run of `update`.

```sh
shulker outdated
```

### `shulker suggests`

List the mods that locked mods recommend or suggest in their metadata and that aren't installed, grouped by the mod that names them. Optional dependencies, mostly integrations with other mods, are left out unless you pass `--optional`. An optional dependency that is installed must still match its version range, or `add` and `install` report a problem.

```sh
shulker suggests
shulker suggests --optional
```

| Flag | Description |
| --- | --- |
| `--optional` | Also list optional dependencies, labelled `optional` |

With `--json`, `data.suggestions` lists each one as `{ "mod", "kind", "on", "declared" }`, where `kind` is `recommends`, `suggests`, or `optional`.

### `shulker pin`

Pin a mod to a provider version id. Without a version, pins it to the version already in the lock.

```sh
shulker pin iris k9RhZq2X
shulker pin sodium
```

### `shulker unpin`

Remove a mod's pin and re-resolve it.

```sh
shulker unpin iris
```

## Targets

A target is one build of the project: a client instance or a server directory. Targets never change the lock.

### `shulker target add`

Add a target to `shulker.json`. It doesn't build anything; run `shulker build <name>` next. A target named `client` or `server` gets that side; any other name needs `--side`.

```sh
shulker target add server
shulker target add shaders --side client --feature shaders --name "Shaders Client"
```

| Flag | Description |
| --- | --- |
| `--side <side>` | `client` or `server` (default: the target name when it is `client` or `server`) |
| `--build <dir>` | Output directory (default: `build/<name>`) |
| `--overrides <dir>` | Override layer, applied in order; repeat for more (default: `overrides`) |
| `--feature <name>` | Feature on by default for this target; repeat for more |
| `--whole-file <path>` | `.properties` override path or glob to copy whole instead of merging per key; repeat for more |
| `--name <name>` | Display name launchers show (default: the manifest name) |
| `--var <key=value>` | Template variable; repeat for more |
| `--note <text>` | Free-form note kept in `shulker.json` |

### `shulker target remove`

Remove a target from `shulker.json`. Its build directory stays on disk. The last target can't be removed; add its replacement first. Alias: `rm`.

```sh
shulker target remove shaders
```

### `shulker target list`

List targets with their side, build directory, overrides, features, and display name. Alias: `ls`.

```sh
shulker target list
```

## Settings

`set`, `unset`, and `get` edit and read any field of `shulker.json` by its dotted path, like `server.eula` or `mods.sodium.channel`. They never change `shulker.lock`. When an edit leaves the lock out of date, they warn and name each difference; `shulker lock` brings it back in line.

Inside a map of plain values (`server.properties`, `variables`, `client.options`, `links`), everything after the map's name is the key, so `server.properties.rcon.port` needs no escaping.

With `--json`, `set` and `unset` return `{ "path", "from", "to" }`, leaving out `from` when the field wasn't set and `to` after `unset`. `get` returns the value itself.

### `shulker set`

Set a field. A plain value becomes the most specific type the field allows: `true` and `false` are booleans and `25565` is a number where the field takes one; anything else is a string. Lists, objects, and a value that must stay a string take JSON with `--literal`. For `server.players.whitelist`, `ops`, and `bans`, a player name or uuid adds that player to the list.

The edited `shulker.json` is checked against the schema before anything is written, and the error names the field.

```sh
shulker set server.eula true
shulker set server.properties.max-players 20
shulker set variables.zip --literal '"02134"'
shulker set server.jvmArgs --literal '["-XX:+UseZGC"]'
shulker set server.players.ops Notch
```

| Flag | Description |
| --- | --- |
| `--literal` | Parse the value as JSON |

### `shulker unset`

Remove a field. Removing a field that isn't set succeeds and says so.

```sh
shulker unset server.memory
```

### `shulker get`

Print a field: a string as it is, anything else as JSON. With no path, print all of `shulker.json`. A field that isn't set fails with `path-not-set`.

```sh
shulker get name
shulker get server.properties
```

## Features

A feature is a name that mods opt into with a `feature` condition, like `shaders`. Each target can turn features on by default. Your own choices are saved in `shulker.local.json` next to `shulker.json`. That file is per machine and is added to `.gitignore`. `build`, `install`, `sync`, and `export mrpack` use your choices over the target defaults, and their `--with` and `--without` flags override both for one run.

A directory you sync into, such as a launcher instance, can have its own choices in its own `shulker.local.json`. Set them with `--into <dir>`, or with `--instance <name>` for anything [`shulker links`](#shulker-links) lists. When you sync into it, its choices beat the project's, and `--with` and `--without` still beat both.

### `shulker feature on|off`

Turn a feature on or off for every target on this machine. It takes effect on the next build or sync, including a launcher's pre-launch sync. Naming a feature nothing in `shulker.json` uses is an error.

With `--into`, the choice is saved for that synced directory only. shulker checks the name against the project that directory was synced from.

```sh
shulker feature on shaders
shulker feature off fancy
shulker feature on shaders --into ~/instances/my-pack --sync
shulker feature on shaders --instance "Friends SMP"
```

| Flag | Description |
| --- | --- |
| `--into <path>` | Change the choice for a directory you synced into, instead of this project |
| `--instance <name>` | Change the choice for a linked instance or synced directory, by name or directory |
| `--launcher <launcher>` | Only match `--instance` against entries linked in this launcher: `prism`, `multimc`, or `mojang` |
| `--side <side>` | Only match `--instance` against `client` or `server` entries |
| `--sync` | Sync the directory from its source right away, instead of at the next sync |

### `shulker feature reset`

Forget your choice for a feature so it follows the target defaults again.

```sh
shulker feature reset shaders
shulker feature reset shaders --into ~/instances/my-pack
```

| Flag | Description |
| --- | --- |
| `--into <path>` | Forget the choice for a directory you synced into, instead of this project |
| `--instance <name>` | Forget the choice for a linked instance or synced directory, by name or directory |
| `--launcher <launcher>` | Only match `--instance` against entries linked in this launcher: `prism`, `multimc`, or `mojang` |
| `--side <side>` | Only match `--instance` against `client` or `server` entries |
| `--sync` | Sync the directory from its source right away, instead of at the next sync |

### `shulker feature list`

List each feature with its state and the mods it gates. A `!` before a mod means the mod ships only while the feature is off. Alias: `ls`.

```sh
shulker feature list
shulker feature list --into ~/instances/my-pack
```

| Flag | Description |
| --- | --- |
| `--into <path>` | List the choices that apply to a directory you synced into |
| `--instance <name>` | List the choices that apply to a linked instance or synced directory, by name or directory |
| `--launcher <launcher>` | Only match `--instance` against entries linked in this launcher: `prism`, `multimc`, or `mojang` |
| `--side <side>` | Only match `--instance` against `client` or `server` entries |

## Builds

### `shulker install`

Download everything in the lock and build every target. Run this after cloning a project.

```sh
shulker install
```

| Flag | Description |
| --- | --- |
| `--force` | Overwrite files edited in the build directory |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |

### `shulker build`

Assemble a target's build directory from the lock and its overrides. With no target, builds all of them.

```sh
shulker build
shulker build client
```

| Flag | Description |
| --- | --- |
| `--target <name>` | Target to build (default: every target); the same as the argument, and passing both is an error |
| `--force` | Overwrite files edited in the build directory |
| `--accept-player-change` | Relock a player name that now belongs to a different account |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |

### `shulker diff`

Show files in a build directory that differ from what `build` would write, such as config changed in-game. With no target, checks all of them.

```sh
shulker diff
shulker diff client
shulker diff server --into /srv/minecraft
```

| Flag | Description |
| --- | --- |
| `--target <name>` | Target to diff (default: every target); the same as the argument, and passing both is an error |
| `--into <path>` | Directory the target was synced into (default: the build directory and every directory `sync` recorded) |

### `shulker pull`

Copy edits made in a build directory back into their source, an override file or keys in shulker.json, so the next build keeps them. With no files, pulls every changed file. Paths are relative to the build directory. For a `.properties` override, only the keys it lists are pulled; name more with `--key` to start managing them.

```sh
shulker pull
shulker pull config/sodium-options.json --target client
shulker pull config/iris.properties --key colorSpace
```

| Flag | Description |
| --- | --- |
| `--target <name>` | Target whose build directory to pull from (default: the only target) |
| `--into <path>` | Directory the target was synced into (default: the build directory and every directory `sync` recorded) |
| `--key <key>` | Start managing this key of the one named `.properties` file, copying its current value into the override; repeat for more |

## Running

### `shulker serve`

Build a server target and run it in the foreground. It downloads whatever the lock needs first, the way `install` does, so a fresh clone reaches a running server in one command.

```sh
shulker serve
shulker serve server --accept-eula
```

| Flag | Description |
| --- | --- |
| `--target <name>` | Server target to run (default: the only server target); the same as the argument, and passing both is an error |
| `--force` | Overwrite files edited in the build directory |
| `--accept-eula` | Record acceptance of the Minecraft EULA in shulker.json without prompting |

### `shulker link mojang`

Install the loader into the official launcher and add a profile that points at the client build. Alias: `vanilla`.

```sh
shulker link mojang
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher directory (default: the official launcher's `.minecraft` folder) |
| `--target <name>` | Client target to link (default: the only client target) |

### `shulker link prism`

Create a Prism Launcher or MultiMC instance that syncs the client build before each launch. Alias: `multimc`.

With no source, it links the project in the current directory. Pass a project directory, git URL, or manifest URL to link that instead. shulker then syncs the instance right away, so it's ready to play, and keeps it up to date from the same source before each launch. Nothing is created in the directory you ran it from.

`--with` and `--without` are saved in the instance's own `shulker.local.json`. Change them later with `shulker feature on|off --into <game dir>`, or run `link` again with new flags.

If the instance already syncs from a different source, `link` fails rather than repointing it. Use `--name` to create a second instance, or `--force` to repoint this one. On the next sync, files the old source put there are removed, unless you changed them in-game.

```sh
shulker link prism
shulker link prism https://github.com/shulker-sh/base-pack.git
shulker link prism https://example.com/pack/shulker.json --name "Friends SMP" --with shaders
shulker link prism --mode symlink
shulker link multimc --launcher-dir ~/MultiMC
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher data directory (default: Prism Launcher's; required for MultiMC) |
| `--target <name>` | Client target to link (default: the only client target) |
| `--mode <mode>` | `sync`: build into the instance before each launch; `symlink`: point the instance at the build directory (local projects only) |
| `--name <name>` | Instance name (default: the target's display name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Repoint an instance that syncs from a different source |
| `--with <feature>` | Turn a feature on for this instance; repeat for more (sync mode only) |
| `--without <feature>` | Turn a feature off for this instance; repeat for more (sync mode only) |

### `shulker sync`

Download and build one target of a project straight into a directory, without setting up a project there. The source can be a project directory, a git URL, or a manifest URL. Worlds, logs, screenshots and crash reports stay in the directory you sync into, and nothing is written into the source project; only the project's own build directories link them to its `data/<target>/`.

```sh
shulker sync https://github.com/shulker-sh/base-pack.git --target server --into /srv/minecraft
shulker sync ../my-pack --target client --into ~/instances/my-pack
shulker sync --instance "Friends SMP"
shulker sync --into ~/instances/my-pack
shulker sync --all --side server
shulker sync
```

With `--into` and no source, shulker reads what the directory was last synced from out of its own `.shulker-state.json`, so a synced directory keeps working even if the links registry is gone.

If a git or manifest URL can't be reached because the network is down, `sync` warns and builds from the copy used by the last sync from that source that succeeded, so an instance still launches offline. The warning names the commit and says how old that copy is. A server that answers with an error, a missing ref, or a failed login still fails the sync, and so does a source that has never synced successfully here. `--offline` skips the network entirely, which is quicker than waiting for timeouts on a network that drops traffic. For a server target, an installed Java runtime is kept when its update check can't reach the network.

shulker keeps a list of the directories it syncs into, in its `config.json`. A `sync --into` adds the directory to that list, named after the target's display name (or `--name`), along with its source, target, and ref. `link` does the same for each launcher instance or profile. Syncing into the target's own build directory adds nothing. [`shulker links`](#shulker-links) shows the list.

To update something on that list, name it instead of a source. `--instance` takes an entry's name or directory and syncs it from its recorded source, target, and ref. If several entries have that name, narrow it with `--launcher` or `--side`, or pass `--all` to sync them all. `--all` alone syncs every entry. It keeps going when one fails, and exits with an error at the end. With no source and neither flag, `sync` asks which entry to sync when run in a terminal, and fails with the list otherwise.

| Flag | Description |
| --- | --- |
| `--target <name>` | Target to build (default: the only target) |
| `--into <path>` | Output directory (default: the target's build directory) |
| `--name <name>` | Name to list the `--into` directory under (default: the target's display name; kept on later syncs) |
| `--instance <name>` | Sync a linked instance or synced directory, by name or directory, instead of a source |
| `--all` | Sync every entry `--instance` matches, or every entry when there's no `--instance` |
| `--launcher <launcher>` | Only entries linked in this launcher: `prism`, `multimc`, or `mojang` |
| `--side <side>` | Only `client` or `server` entries |
| `--offline` | Don't use the network; build from the last successful sync and cached files |
| `--force` | Overwrite files edited in the output directory |
| `--ref <ref>` | Branch, tag, or commit to sync from a git source (default: the remote HEAD) |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |

### `shulker links`

List the launcher instances and directories shulker keeps in sync, grouped by launcher, with plain `sync --into` directories last. Each entry shows its side, when it was last synced, its directory, and the source and target it syncs from. A directory that is gone or can't be read is flagged.

```sh
shulker links
```

```
Prism Launcher
  Friends SMP (client), synced 2026-09-11 14:02
    ~/Library/Application Support/PrismLauncher/instances/shulker-friends-smp/minecraft
    from https://github.com/shulker-sh/base-pack.git, target client

Other directories
  My Pack server (server), synced 2026-09-10 21:40
    /srv/minecraft
    from https://github.com/shulker-sh/base-pack.git, ref v3, target server
```

### `shulker unlink`

Stop syncing a linked instance or synced directory and remove it from the list. Its files, worlds, and feature choices stay. For a Prism Launcher or MultiMC instance, `unlink` removes the pre-launch sync but keeps the instance. It leaves a pre-launch command alone if you replaced shulker's with your own. For the official launcher, it removes the profile but keeps the build directory and the installed loader. A plain synced directory is just forgotten.

Name the entry the way [`shulker links`](#shulker-links) shows it, or pass its directory. A name several entries share needs `--launcher`, `--side`, or `--all`. `unlink` prints the command that sets the entry up again.

```sh
shulker unlink "Friends SMP"
shulker unlink "My Pack" --launcher prism
shulker unlink --all --side server
```

| Flag | Description |
| --- | --- |
| `--all` | Unlink every entry the name matches, or every entry when there's no name |
| `--launcher <launcher>` | Only entries linked in this launcher: `prism`, `multimc`, or `mojang` |
| `--side <side>` | Only `client` or `server` entries |

## Packs

A pack is another shulker project whose mods and overrides merge into this one.

### `shulker pack add`

Add a pack from a local path, git URL, or raw manifest URL.

```sh
shulker pack add https://github.com/shulker-sh/base-pack.git --ref v3
shulker pack add ../base-pack
```

| Flag | Description |
| --- | --- |
| `--ref <ref>` | Branch, tag, or commit for git sources |
| `--name <name>` | Name used in messages and `requiredBy` (default: derived from the source) |

### `shulker pack remove`

Remove a pack and prune the mods only it provided. Alias: `rm`.

```sh
shulker pack remove base-pack
```

### `shulker pack list`

List packs with their locked ref and whether a local pack has changed. Alias: `ls`.

```sh
shulker pack list
```

## Players

### `shulker player`

Check player names and uuids against Mojang and the lock. Each player is reported as ok, renamed (same uuid, new name), reassigned (same name, different account), or unknown. Renames are recorded in the lock; reassignments wait for `build --accept-player-change`.

```sh
shulker player Notch
shulker player --all
```

| Flag | Description |
| --- | --- |
| `--all` | Check every player in the manifest |

## Other

### `shulker version`

Print the shulker version.

```sh
shulker version
```

### `shulker self update`

Replace the running shulker with the latest release from GitHub. It checks the download against the release's SHA256 checksums and, when the [GitHub CLI](https://cli.github.com) (`gh`) is installed, verifies its build provenance. Without `gh`, it installs on the checksum alone.

```sh
shulker self update
shulker self update --check
```

| Flag | Description |
| --- | --- |
| `--check` | Only report whether a newer release is available |
| `--without-attestation` | Skip the build provenance check |
| `--require-attestation` | Fail unless `gh` verifies the build provenance |

## JSON output

With `--json`, every command prints one JSON object on stdout, whether it succeeds or fails:

```json
{
  "ok": true,
  "command": "pack add",
  "lockStale": false,
  "warnings": [],
  "data": {}
}
```

| Field | Description |
| --- | --- |
| `ok` | `true` when the command succeeded |
| `command` | The command that ran, like `pack add` |
| `lockStale` | `shulker.lock` doesn't match `shulker.json`; `shulker lock` brings it in line. Commands that build from the lock warn, naming each difference, and carry on; `export` refuses |
| `warnings` | Everything shulker would print as `warning:` without `--json`. Always present, empty when there are none |
| `data` | The command's result. When a command that works through several entries fails, like `sync --all`, it holds the result for each entry |
| `error` | Present when `ok` is `false`: `code`, `message`, and sometimes `candidates` or `items` |

`candidates` lists values you could pass instead, like the target names when `--target` matches none of them. `items` lists what the error is about, like the files in conflict. Both are left out when empty.

| Exit status | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Failure; `error.code` says which |
| `2` | Usage: an unknown command or flag, wrong arguments, or a flag value that isn't allowed |
| `130` | Interrupted (`interrupted`) |

`serve` exits with the server's own status when the server fails (`server-exit`).

### Lock changes

`lock`, `add`, `remove`, `update`, `pin`, `unpin`, `pack add`, and `pack remove` all return the same `data`: what changed in `shulker.lock` and `shulker.json`.

```json
{
  "reresolved": [],
  "platform": [],
  "added": [{ "id": "fabric-api", "versionNumber": "0.119.0", "side": "both", "provider": "modrinth", "requiredBy": ["sodium"] }],
  "updated": [{ "id": "lithium", "from": "0.14.1", "to": "0.14.3" }],
  "removed": [{ "id": "iris", "versionNumber": "1.8.0", "requiredBy": [] }],
  "packs": [{ "name": "base", "from": "abc1234", "to": "def5678" }],
  "suggestions": []
}
```

| Field | Description |
| --- | --- |
| `reresolved` | Why every mod was resolved again, one difference per entry, like `minecraft: locked 26.1 is outside ~26.2`. Empty when only some mods changed |
| `platform` | `minecraft` and `loader` when their locked version changed, as `{ "id", "from", "to" }`. `from` is empty for a new lock |
| `added` | Mods newly locked. `requiredBy` names the mods and packs that pulled one in; empty when only `shulker.json` lists it. `alreadyLocked` marks a dependency that `add` just listed in `shulker.json` |
| `updated` | Mods whose locked version changed. `fromProvider` and `toProvider` appear when the provider changed |
| `removed` | Mods no longer locked, with the `requiredBy` they had. `stillLocked` marks a mod taken out of `shulker.json` that a pack still provides |
| `packs` | Packs added, removed, or moved to another commit. `from` is empty for a new pack, `to` for a removed one |
| `suggestions` | Recommended mods that aren't installed |
| `pin` | `pin` only: the version it pinned to |

### Error codes

| Code | Meaning |
| --- | --- |
| `ambiguous-instance` | Several linked instances or synced directories match. `candidates`: the matches |
| `ambiguous-into` | The target has edits in several synced directories; pass `--into`. `candidates`: the directories |
| `ambiguous-target` | Several targets fit; pass `--target`. `candidates`: the targets |
| `build-conflict` | Files changed both in the build directory and in the source; run `diff`, or pass `--force` to overwrite. `items`: the files |
| `config-invalid` | shulker's `config.json` isn't valid JSON |
| `error` | Anything unexpected, like a file that can't be read or written. The message has the details |
| `eula-required` | The server needs the Minecraft EULA accepted |
| `feature-not-found` | No mod or target uses the feature. `candidates`: the features in use |
| `file-not-found` | A file named to `pull` isn't in the build directory |
| `git-missing` | A git source needs `git` on PATH |
| `id-changed` | A new version of a mod identifies itself as a different mod |
| `installer-failed` | NeoForge's or Forge's own installer failed while setting up a server dir; the message ends with its last output |
| `instance-dir-not-empty` | The instance directory already has files |
| `instance-exists` | An instance already syncs from this source; pass `--name` for a second one, or `--force` |
| `instance-missing` | A linked instance's directory is gone |
| `source-unknown` | `sync --into` found no record in the directory of what it was synced from; name the source |
| `instance-not-found` | Nothing linked matches. `candidates`: the linked entries |
| `interrupted` | Ctrl-C or SIGTERM stopped the command. Files are left whole: each one is written in full or not at all. A second Ctrl-C quits at once |
| `into-required` | Syncing from a remote source needs `--into` |
| `into-target` | `--into` applies to one target; name it |
| `java-not-found` | No working Java at the configured path or on PATH |
| `java-range` | `java` in `shulker.json` is neither a path nor a version range |
| `java-version` | The Java found is outside the range in `shulker.json` |
| `jvm-flags` | Unknown `jvmFlags` preset |
| `key-not-found` | A `--key` isn't in the file. `candidates`: its keys |
| `last-target` | The only target can't be removed |
| `launcher-dir-required` | MultiMC needs `--launcher-dir` |
| `launcher-not-found` | No launcher directory where shulker looked |
| `loader-install-incomplete` | The loader's installer left no launcher profile to read the installed version from |
| `local-invalid` | `shulker.local.json` isn't valid JSON |
| `lock-invalid` | `shulker.lock` doesn't parse or match its schema, or a change would make it invalid |
| `lock-not-found` | No `shulker.lock`; run `shulker lock` |
| `lock-stale` | `export` needs a lock that matches `shulker.json`; run `shulker lock`. Other commands only warn. `items`: each difference |
| `manifest-exists` | A `shulker.json` is already where `init` or `import` would write one |
| `manifest-invalid` | `shulker.json` doesn't parse or match its schema, or a change would make it invalid |
| `manifest-not-found` | No `shulker.json` in the project directory or the sync source |
| `manual-download` | The provider doesn't distribute this mod; download it into `downloads/` |
| `memory` | Server memory isn't a whole number of M or G |
| `missing-files` | Mods that need a manual download are missing. `items`: what to download |
| `mod-not-found` | The mod isn't on any provider, or isn't in `shulker.json`. `candidates`: the mods in `shulker.json`, where relevant |
| `mrpack-download` | A file in the modpack couldn't be downloaded |
| `mrpack-host-not-allowed` | Modrinth launchers won't download these mods; pass `--bundle`. `items`: the mods |
| `mrpack-invalid` | The modpack is malformed |
| `mrpack-marker` | The modpack's shulker marker can't be read |
| `mrpack-unsupported` | The modpack's format or loader isn't supported |
| `no-compatible-version` | The mod has no version for this Minecraft and loader. `candidates`: other release channels that have one |
| `no-links` | Nothing is linked yet |
| `no-overrides` | The target has no overrides directory to adopt a file into |
| `no-target` | `shulker.json` has no target of the side the command needs |
| `not-built` | The target has no build directory yet; run `shulker build` |
| `not-direct` | The mod is only a dependency. `items`: the mods that require it |
| `not-drifted` | A file named to `pull` has no changes. `candidates`: the changed files |
| `not-installed` | A file isn't in the cache; run `shulker install` |
| `not-pinned` | The mod has no pin |
| `not-synced` | The directory has no record of the source it was synced from |
| `pack-changed` | A pack no longer matches the lock; run `shulker update` |
| `pack-conflict` | Two packs list the same mod with different settings |
| `pack-exists` | The pack is already in `shulker.json` |
| `pack-fetch` | A pack couldn't be fetched |
| `pack-manifest` | A pack source has no `shulker.json` |
| `pack-mismatch` | A pack wants a different Minecraft version or loader |
| `pack-name` | A pack's name can't be worked out, or two packs share one; set `name` |
| `pack-not-found` | The pack isn't in `shulker.json`. `candidates`: the packs |
| `pack-provided` | The mod comes from a pack, so it can't be removed on its own |
| `pack-ref` | A pack's `ref` doesn't apply to its source, or wasn't found |
| `pack-target` | A pack has several targets of a side and none named like the project's |
| `pack-unlocked` | A pack has no commit in the lock; run `shulker update` |
| `path-invalid` | `shulker.json` has no such field, or the path goes inside a single value or a list. `candidates`: the fields allowed there |
| `path-not-set` | `get` names a field that isn't set |
| `pin-mismatch` | The pinned version belongs to a different project |
| `player-invalid` | Neither a player name nor a uuid |
| `player-reassigned` | Player names now belong to different accounts; pass `--accept-player-change`. `items`: the players |
| `player-unknown` | Players that don't exist at Mojang. `items`: the names |
| `player-unresolved` | A player isn't in the lock; run `shulker player` |
| `players-invalid` | A player entry in `shulker.json` is invalid |
| `properties-invalid` | `server.properties` keys that aren't valid for this Minecraft version. `items`: the keys |
| `provider-unavailable` | The provider isn't set up, like CurseForge without an API key |
| `runtime-unavailable` | Mojang publishes no Java runtime for this platform; set `java` in `shulker.json` |
| `self-update-check` | Checking for a release failed, or none is published |
| `self-update-checksum` | The download doesn't match its checksum |
| `self-update-download` | The download failed |
| `self-update-install` | The running binary couldn't be replaced |
| `self-update-provenance` | `--require-attestation` is set and the build provenance couldn't be verified |
| `server-exit` | The server exited with an error |
| `source-fetch` | The sync source couldn't be fetched |
| `source-lock` | The sync source has no `shulker.lock` |
| `source-offline` | Offline, and the source has never synced here, so there's no copy to use |
| `source-ref` | `--ref` doesn't apply to the source, or wasn't found |
| `sync-failed` | Some entries failed to sync; `data` has each entry's result |
| `target-exists` | The target is already in `shulker.json` |
| `target-not-found` | No such target. `candidates`: the targets |
| `unlink-failed` | Some entries couldn't be unlinked; `data` has each entry's result |
| `unset-variable` | An override uses a variable that isn't set |
| `unsupported-loader` | shulker doesn't support the loader yet |
| `unsupported-mode` | `--mode symlink` isn't supported on Windows yet |
| `usage` | An unknown command or flag, wrong arguments, or a flag value that isn't allowed. Exits 2 |
| `validation-failed` | The locked mods have dependency problems. `items`: the problems |
| `version-required` | `export mrpack` needs a version |
| `wrong-side-target` | The target is on the wrong side for the command. `candidates`: the targets on the right side |
