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

Download and build one target of a project straight into a directory, without setting up a project there. The source can be a project directory, a git URL, or a manifest URL.

```sh
shulker sync https://github.com/shulker-sh/base-pack.git --target server --into /srv/minecraft
shulker sync ../my-pack --target client --into ~/instances/my-pack
shulker sync --instance "Friends SMP"
shulker sync --all --side server
shulker sync
```

If a git or manifest URL can't be reached, `sync` warns and builds from the copy it fetched last time, so an instance still launches offline. It fails only when that source has never been fetched.

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
