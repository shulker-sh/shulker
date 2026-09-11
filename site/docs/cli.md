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
| [`shulker update [mod...]`](#shulker-update) | Update mods to the newest compatible version |
| [`shulker outdated [mod...]`](#shulker-outdated) | Show mods with a newer compatible version |
| [`shulker pin <mod> [version]`](#shulker-pin) | Pin a mod to a provider version id |
| [`shulker unpin <mod>`](#shulker-unpin) | Remove a mod's pin and re-resolve it |
| [`shulker target add <name>`](#shulker-target-add) | Add a build target |
| [`shulker target remove <name>`](#shulker-target-remove) | Remove a target, leaving its build directory |
| [`shulker target list`](#shulker-target-list) | List targets |
| [`shulker feature on\|off <feature>`](#shulker-feature-on-off) | Turn a feature on or off on this machine |
| [`shulker feature reset <feature>`](#shulker-feature-reset) | Go back to the target defaults for a feature |
| [`shulker feature list`](#shulker-feature-list) | List features and whether they're on |
| [`shulker install`](#shulker-install) | Download everything in the lock and build all targets |
| [`shulker build [target]`](#shulker-build) | Assemble build directories from the lock and overrides |
| [`shulker diff [target]`](#shulker-diff) | Show build files that differ from what build would write |
| [`shulker pull [file...]`](#shulker-pull) | Copy edits made in a build directory back into their source |
| [`shulker serve`](#shulker-serve) | Build a server target and run it in the foreground |
| [`shulker link mojang`](#shulker-link-mojang) | Add a profile for the client build to the official launcher |
| [`shulker link prism`](#shulker-link-prism) | Create a Prism Launcher or MultiMC instance for the client build |
| [`shulker sync <source>`](#shulker-sync) | Download and build one target of a project into a directory |
| [`shulker pack add <source>`](#shulker-pack-add) | Add a pack from a local path, git URL, or manifest URL |
| [`shulker pack remove <name>`](#shulker-pack-remove) | Remove a pack |
| [`shulker pack list`](#shulker-pack-list) | List packs and their local drift state |
| [`shulker player [name\|uuid]...`](#shulker-player) | Check player names and uuids against Mojang and the lock |
| [`shulker import mrpack <file>`](#shulker-import-mrpack) | Create a project from a Modrinth modpack |
| [`shulker export mrpack`](#shulker-export-mrpack) | Export a Modrinth modpack |
| [`shulker version`](#shulker-version) | Print the shulker version |

## Global flags

These work with every command.

| Flag | Description |
| --- | --- |
| `-C, --dir <path>` | Project directory (default: current directory) |
| `--json` | Print machine-readable JSON, including errors |

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
shulker import mrpack pack.mrpack --dir my-pack --name my-pack
```

| Flag | Description |
| --- | --- |
| `--dir <path>` | Project directory to create (default: `./<name>`) |
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

## Features

A feature is a name that mods opt into with a `feature` condition, like `shaders`. Each target can turn features on by default. Your own choices are saved in `shulker.local.json` next to `shulker.json`. That file is per machine and is added to `.gitignore`. `build`, `install`, `sync`, and `export mrpack` use your choices over the target defaults, and their `--with` and `--without` flags override both for one run.

### `shulker feature on|off`

Turn a feature on or off for every target on this machine. It takes effect on the next build or sync, including a launcher's pre-launch sync. Naming a feature nothing in `shulker.json` uses is an error.

```sh
shulker feature on shaders
shulker feature off fancy
```

### `shulker feature reset`

Forget your choice for a feature so it follows the target defaults again.

```sh
shulker feature reset shaders
```

### `shulker feature list`

List each feature with its state and the mods it gates. A `!` before a mod means the mod ships only while the feature is off. Alias: `ls`.

```sh
shulker feature list
```

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
| `--force` | Overwrite files edited in the build directory and ignore a stale lock |
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

Build a server target and run it in the foreground.

```sh
shulker serve
shulker serve --target server --accept-eula
```

| Flag | Description |
| --- | --- |
| `--target <name>` | Server target to run (default: the only server target) |
| `--force` | Overwrite files edited in the build directory and ignore a stale lock |
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

```sh
shulker link prism
shulker link prism --mode symlink
shulker link multimc --launcher-dir ~/MultiMC
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher data directory (default: Prism Launcher's; required for MultiMC) |
| `--target <name>` | Client target to link (default: the only client target) |
| `--mode <mode>` | `sync`: build into the instance before each launch; `symlink`: point the instance at the build directory |
| `--with <feature>` | Turn a feature on in every pre-launch sync of this instance; repeat for more (sync mode only) |
| `--without <feature>` | Turn a feature off in every pre-launch sync of this instance; repeat for more (sync mode only) |

### `shulker sync`

Download and build one target of a project straight into a directory, without setting up a project there. The source can be a project directory, a git URL, or a manifest URL.

```sh
shulker sync https://github.com/shulker-sh/base-pack.git --target server --into /srv/minecraft
shulker sync ../my-pack --target client --into ~/instances/my-pack
```

| Flag | Description |
| --- | --- |
| `--target <name>` | Target to build (default: the only target) |
| `--into <path>` | Output directory (default: the target's build directory) |
| `--force` | Overwrite files edited in the output directory |
| `--ref <ref>` | Branch, tag, or commit to sync from a git source (default: the remote HEAD) |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |

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
