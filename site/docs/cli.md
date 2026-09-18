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
| [`shulker ignore <mod> <on>`](#shulker-ignore) | Record that a dependency problem is safe to ignore |
| [`shulker unignore <mod> <on>`](#shulker-unignore) | Drop an ignored dependency problem |
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
| [`shulker history list`](#shulker-history-list) | List the states kept before in-place builds |
| [`shulker history show [n]`](#shulker-history-show) | Show a history entry and what restoring it would change |
| [`shulker history prune`](#shulker-history-prune) | Remove history entries beyond the number the manifest keeps |
| [`shulker rollback [n]`](#shulker-rollback) | Restore a history entry and build it in place |
| [`shulker serve [target]`](#shulker-serve) | Build a server target and run it in the foreground |
| [`shulker link atlauncher [source]`](#shulker-link-atlauncher) | Create an ATLauncher instance for the client build |
| [`shulker link gdlauncher [source]`](#shulker-link-gdlauncher) | Create a GDLauncher instance for the client build |
| [`shulker link mojang [source]`](#shulker-link-mojang) | Add a profile for the client build to the official launcher |
| [`shulker link prism [source]`](#shulker-link-prism) | Create a Prism Launcher or MultiMC instance for the client build |
| [`shulker sync [source]`](#shulker-sync) | Download and build one target of a project into a directory, or update a linked one |
| [`shulker instances`](#shulker-instances) | List the instances shulker keeps in sync |
| [`shulker instances repair`](#shulker-instances-repair) | Register instances shulker has lost track of and write any missing instance files |
| [`shulker unlink <name>`](#shulker-unlink) | Stop syncing a linked instance or synced directory, keeping its files |
| [`shulker list`](#shulker-list) | List everything in `requires` with its locked version |
| [`shulker mod add\|remove\|list`](#shulker-mod-add-remove-list) | The plain verbs with `--type mod` |
| [`shulker modpack add\|remove\|list`](#shulker-modpack-add-remove-list) | Manage modpacks whose mods and overrides merge into this project |
| [`shulker resourcepack add\|remove\|list`](#shulker-resourcepack-add-remove-list) | The plain verbs with `--type resourcepack` |
| [`shulker shader add\|remove\|list`](#shulker-shader-add-remove-list) | The plain verbs with `--type shader` |
| [`shulker player [name\|uuid]...`](#shulker-player) | Check player names and uuids against Mojang and the lock |
| [`shulker import mrpack <file>`](#shulker-import-mrpack) | Create a project from a Modrinth modpack |
| [`shulker export mrpack [source]`](#shulker-export-mrpack) | Export a Modrinth modpack |
| [`shulker export curseforge [source]`](#shulker-export-curseforge) | Export a CurseForge modpack |
| [`shulker docs [topic]...`](#shulker-docs) | Print shulker's documentation |
| [`shulker cache info`](#shulker-cache-info) | Show the cache's size and how much prune would free |
| [`shulker cache prune`](#shulker-cache-prune) | Remove cached files no instance or project references |
| [`shulker version`](#shulker-version) | Print the shulker version |
| [`shulker self update`](#shulker-self-update) | Update shulker to the latest release |
| [`shulker completion bash`](#shulker-completion-bash) | Print the bash completion script |
| [`shulker completion zsh`](#shulker-completion-zsh) | Print the zsh completion script |
| [`shulker completion fish`](#shulker-completion-fish) | Print the fish completion script |
| [`shulker completion powershell`](#shulker-completion-powershell) | Print the PowerShell completion script |

## Global flags

These work with every command.

| Flag | Description |
| --- | --- |
| `-C, --dir <path>` | Project directory (default: current directory) |
| `-i, --instance <id>` | Act on a registered instance instead of a project directory, by id, name, or directory; `--id` is accepted as an alias. Can't be combined with `-C`. [`shulker instances`](#shulker-instances) lists them |
| `--json` | Print machine-readable JSON, including errors; see [JSON output](#json-output) |
| `--no-color` | Print without colour. Setting `NO_COLOR` or `TERM=dumb` does the same, and colour is off whenever the output is not a terminal |
| `--ascii` | Print with ASCII glyphs (`*`, `x`, `|-`, `->`, `>>`) in place of `✔`, `✘`, `├─`, `⟶`, and `»` |

## Projects

### `shulker init`

Create `shulker.json` and `shulker.lock` in the current directory. Pass `--yes` for the defaults, or set at least `--name` and `--minecraft`.

```sh
shulker init --yes
shulker init --name my-server --minecraft 1.21.1 --loader neoforge --target server
```

| Flag | Description |
| --- | --- |
| `-y, --yes` | Accept defaults: latest release, no loader, client target |
| `--name <name>` | Project name (default: directory name) |
| `--minecraft <version>` | Minecraft version or range (default: latest release) |
| `--loader <loader>` | Mod loader: `none` (the default, vanilla Minecraft), `fabric`, `quilt`, `neoforge`, `forge` |
| `--loader-version <range>` | Loader version range (default: `*`); needs `--loader` |
| `--target <side>` | First target: `client` or `server` |

### `shulker import mrpack`

Create a project from a Modrinth modpack (`.mrpack`). A pack shulker exported carries its own `shulker.json` and `shulker.lock` at the archive root, and those are read in preference to the marker jar, so the project comes back as it was, resource packs and shaders included. `--ignore-shulker` skips both and imports the archive as any other Modrinth modpack.

```sh
shulker import mrpack ~/Downloads/fabulously-optimized.mrpack
shulker import mrpack pack.mrpack -C my-pack --name my-pack
```

| Flag | Description |
| --- | --- |
| `--name <name>` | Project name (default: the modpack name, slugified) |
| `--ignore-shulker` | Ignore the shulker manifest and lock inside the modpack and import it as any other one |

### `shulker export mrpack`

Export the project as a Modrinth modpack for the Modrinth app and other launchers. Resource packs and shaders go in alongside the mods, as client-only files. The archive carries the project's own `shulker.json` and `shulker.lock` at its root, so [`import mrpack`](#shulker-import-mrpack) restores the project it came from. The source is the project in the current directory, or a project directory, git URL, or manifest URL; a git or URL source is downloaded first and the archive is written to the current directory.

```sh
shulker export mrpack
shulker export mrpack --target client -o dist/my-pack.mrpack
shulker export mrpack https://github.com/me/my-pack.git --ref v1.0
```

| Flag | Description |
| --- | --- |
| `--version <version>` | Version written into the modpack (default: `version` in shulker.json) |
| `-o, --output <path>` | Archive path (default: `build/<name>-<version>.mrpack`, or the current directory for a git or URL source) |
| `--target <name>` | Export one target only (default: every target) |
| `--os <os>` | Include mods gated on this OS: `macos`, `windows`, or `linux` (default: leave them out) |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |
| `--bundle` | Put files that Modrinth launchers can't download inside the archive |
| `--ref <ref>` | Branch, tag, or commit to export from a git source (default: the remote HEAD) |

### `shulker export curseforge`

Export the client target as a CurseForge profile `.zip` for the CurseForge app's Import Profile. Mods, resource packs and shaders locked from CurseForge go in by file ID. Everything else is looked up on CurseForge by its fingerprint, and matches go in by file ID too. Files that aren't on CurseForge fail the export unless `--bundle` ships them inside the archive, which the CurseForge app warns about on import. Server-only mods and files are left out. The profile gets shulker's logo as its image, and the archive carries `shulker.json` and `shulker.lock` at its root. The source works as in [`export mrpack`](#shulker-export-mrpack).

```sh
shulker export curseforge
shulker export curseforge --bundle -o dist/my-pack.zip
```

| Flag | Description |
| --- | --- |
| `--version <version>` | Version written into the modpack (default: `version` in shulker.json) |
| `-o, --output <path>` | Archive path (default: `build/<name>-<version>.zip`, or the current directory for a git or URL source) |
| `--target <name>` | Client target to export (default: the only client target) |
| `--os <os>` | Include mods gated on this OS: `macos`, `windows`, or `linux` (default: leave them out) |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |
| `--bundle` | Put mods that aren't on CurseForge inside the archive, and bundle every mod not from CurseForge when the lookup can't run |
| `--ref <ref>` | Branch, tag, or commit to export from a git source (default: the remote HEAD) |

## Mods

### `shulker add`

Add mods to the manifest, resolve them and their dependencies, and write the lock. Mods are named by their provider slug. With `--type modpack` the argument is a modpack source instead: a local path, git URL, or raw manifest URL. Each type takes only the flags that mean something for it, so `--ref` on a mod or `--side` on a modpack is refused.

```sh
shulker add sodium lithium
shulker add iris --channel beta
shulker add betterthirdperson --provider curseforge --side client
shulker add sodium --as speed
shulker add ../base-pack --type modpack --as base
```

| Flag | Description |
| --- | --- |
| `--type <type>` | What the arguments name: `mod` (default), `modpack`, `resourcepack`, `shader` |
| `--side <side>` | Override side: `client`, `server`, `both` |
| `--channel <channel>` | Least stable channel accepted: `release`, `beta`, `alpha` |
| `--pin <version-id>` | Pin to a provider version id (one mod only) |
| `--provider <provider>` | Provider to use for this mod: `modrinth` or `curseforge` |
| `--ref <ref>` | Branch, tag, or commit for a modpack's git source |
| `--as <key>` | Key used in `requires`, messages, and `requiredBy` (default: a mod's jar id, a modpack source's name) |
| `--unlocked` | Resolve a modpack's mods here instead of copying the versions its lock pins |
| `--no-auto-update` | Keep a modpack at its locked version on `shulker sync`; `shulker update` still moves it |
| `--with-deps` | Move dependency versions the lock holds when a mod being added needs another. One a locked modpack pins is listed in `shulker.json` as it moves, so it no longer follows the modpack |

### `shulker remove`

Remove mods from the manifest and prune dependencies nothing else needs. A key that names a modpack removes the modpack and the mods only it provided. Alias: `rm`.

```sh
shulker remove lithium
shulker remove base --type modpack
```

| Flag | Description |
| --- | --- |
| `--type <type>` | What the arguments name: `mod` (default), `modpack`, `resourcepack`, `shader` |

### `shulker list`

List every `requires` entry under a heading per type, with its locked version and where it comes from. Mods a modpack or another mod pulled in are listed too, with `from <modpack>` or `required by <mods>`. Alias: `ls`.

```sh
shulker list
shulker list --type modpack
```

| Flag | Description |
| --- | --- |
| `--type <type>` | Only entries of one type: `mod`, `modpack`, `resourcepack`, `shader` |

### `shulker lock`

Bring `shulker.lock` in line with `shulker.json` after you edit it by hand, without upgrading anything. Mods new to `shulker.json` are resolved, mods nothing lists or requires are dropped, and a mod whose channel, pin, side, provider, or project changed is picked again. Every other mod keeps its locked version, and modpacks stay at their locked commit unless their `ref` changed. When the locked Minecraft or loader version no longer matches `shulker.json`, or a mod is locked from a provider `shulker.json` no longer lists, every mod is resolved again and `reresolved` says why. Without a `shulker.lock`, `lock` creates one.

`add`, `remove`, `update`, `pin`, `unpin`, `modpack add`, and `modpack remove` do the same before their own change, so a hand edit is never left out of the lock. What they bring in shows up in their output.

```sh
shulker lock
```

### `shulker update`

Re-resolve mods to the newest compatible versions. With no arguments, fetches every modpack again, whatever its `autoUpdate`, and updates every mod; naming a modpack updates it and its mods. In an instance (a project whose target builds into its own directory), `update` then builds the target in place; elsewhere it only writes the lock and `shulker install` builds it. Alias: `upgrade`.

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

### `shulker ignore`

Record that a dependency problem between a mod and what its jar declares about another is safe to ignore. A problem reported by `add`, `remove`, `update`, `lock` or `install` prints the exact command to run, with the rule and the range the jar declares. The entry lands in `ignore` in `shulker.json` and the problem stops failing validation for as long as the jar declares that range; a new version that declares a different range makes the entry stale and the problem comes back. Without `--declared`, the command reads the rule and range from a matching problem in the locked mods. An existing entry for the pair is only replaced with `--force`.

```sh
shulker ignore sodium fabric-api --rule depends --declared ">=2.0.0" --note "works on fabric-api 1.x"
shulker ignore sodium fabric-api --note "works on fabric-api 1.x"
```

| Flag | Description |
| --- | --- |
| `--note <why>` | Why the constraint is safe to ignore; required |
| `--rule <rule>` | The problem's rule, `depends` or `breaks`, as printed with the problem |
| `--declared <range>` | The range the jar declares, as printed with the problem; with it nothing is resolved |
| `--force` | Replace an existing ignore for the pair |

With `--json`, `data` is the entry written, `{ "rule", "mod", "on", "declared", "note" }`, plus `replaced`.

### `shulker unignore`

Drop the ignore for a pair so the problem is checked again. Ignores that come from a modpack are left alone.

```sh
shulker unignore sodium fabric-api
```

With `--json`, `data` is the entry removed.

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
| `--build <dir>` | Output directory (default: `build/<name>`); `.` builds into the project directory itself |
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

`set`, `unset`, and `get` edit and read any field of `shulker.json` by its dotted path, like `server.eula` or `requires.sodium.channel`. They never change `shulker.lock`. When an edit leaves the lock out of date, they warn and name each difference; `shulker lock` brings it back in line.

Inside a map of plain values (`server.properties`, `variables`, `client.options`, `links`), everything after the map's name is the key, so `server.properties.rcon.port` needs no escaping.

With `--json`, `set` and `unset` return `{ "path", "from", "to" }`, leaving out `from` when the field wasn't set and `to` after `unset`. `get` returns the value itself.

### `shulker set`

Set a field. A plain value becomes the most specific type the field allows: `true` and `false` are booleans and `25565` is a number where the field takes one; anything else is a string. Lists, objects, and a value that must stay a string take JSON with `--literal`. For `server.players.whitelist`, `ops`, and `bans`, a player name, uuid, or `name:uuid` adds that player to the list. A player already listed is left alone, except that `name:uuid` fills in whichever half the entry lacks; a half that contradicts the entry is an error.

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

## Configuration

`config get`, `config set`, and `config unset` read and change shulker's own `config.json`, which applies to every project. It lives in your user config directory, or wherever `SHULKER_CONFIG` points. It has two keys:

| Key | Description |
| --- | --- |
| `curseforge.key` | Your CurseForge API key. `SHULKER_CURSEFORGE_KEY` takes priority when it is set |
| `registry` | The file listing linked instances and synced directories: absolute, or relative to the directory holding `config.json`. Without it, `registry.json` beside `config.json` |

The CurseForge key is always shown as its last four characters, like `••••c123`, unless you pass `config get --reveal`. With `--json`, `config set` and `config unset` return `{ "path", "from", "to" }` like `set`, plus `created` when they made a new registry file.

### `shulker config get`

Print a key: a string as it is, anything else as JSON. With no key, print all of `config.json`. `registry` shows the file shulker actually uses, even when the key isn't set. A `curseforge.key` that isn't set fails with `path-not-set`.

```sh
shulker config get
shulker config get registry
shulker config get curseforge.key --reveal
```

| Flag | Description |
| --- | --- |
| `--reveal` | Print `curseforge.key` in full |

### `shulker config set`

Set a key. When `registry` points at a file that doesn't exist, `set` creates it as an empty registry; an existing file must be a valid registry, and an empty file counts. If the current registry has entries the new one lacks, shulker would stop syncing them, so `set` fails with `registry-has-instances` and lists them; `--force` changes it anyway.

```sh
shulker config set curseforge.key "$CURSEFORGE_KEY"
shulker config set registry ~/Dropbox/shulker/registry.json
```

| Flag | Description |
| --- | --- |
| `--force` | Change the registry even if it leaves linked instances or synced directories behind |

### `shulker config unset`

Remove a key. Without `registry`, shulker goes back to `registry.json` beside `config.json`, created when missing, with the same check as `set`. Removing a key that isn't set succeeds and says so.

```sh
shulker config unset curseforge.key
```

| Flag | Description |
| --- | --- |
| `--force` | Change the registry even if it leaves linked instances or synced directories behind |

## Features

A feature is a name that mods opt into with a `feature` condition, like `shaders`. Each target can turn features on by default. Your own choices are saved in `shulker.local.json` next to `shulker.json`. That file is per machine and is added to `.gitignore`. `build`, `install`, `sync`, `export mrpack`, and `export curseforge` use your choices over the target defaults, and their `--with` and `--without` flags override both for one run.

A directory you sync into, such as a launcher instance, can have its own choices in its own `shulker.local.json`. Set them with `--into <dir>`, or with `-i <id>` for anything [`shulker instances`](#shulker-instances) lists. When you sync into it, its choices beat the project's, and `--with` and `--without` still beat both.

### `shulker feature on|off`

Turn a feature on or off for every target on this machine. It takes effect on the next build or sync, including a launcher's pre-launch sync. Naming a feature nothing in `shulker.json` uses is an error.

With `--into`, the choice is saved for that synced directory only. shulker checks the name against the project that directory was synced from.

```sh
shulker feature on shaders
shulker feature off fancy
shulker feature on shaders --into ~/instances/my-pack --sync
shulker feature on shaders -i friends-smp
```

| Flag | Description |
| --- | --- |
| `--into <path>` | Change the choice for a directory you synced into, instead of this project |
| `--launcher <launcher>` | Only match `-i` against instances linked in this launcher: `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--side <side>` | Only match `-i` against `client` or `server` instances |
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
| `--launcher <launcher>` | Only match `-i` against instances linked in this launcher: `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--side <side>` | Only match `-i` against `client` or `server` instances |
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
| `--launcher <launcher>` | Only match `-i` against instances linked in this launcher: `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--side <side>` | Only match `-i` against `client` or `server` instances |

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

Show what was edited in a build directory since `build` wrote it, such as config changed in-game, as a diff from the project to the directory. `build` leaves these files alone and `pull` copies the edits back. A per-key file shows only its managed keys. With no target, checks all of them.

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

### `shulker history list`

List the states an instance kept before it changed, newest first. An entry is taken before anything is rewritten: by `add`, `remove`, `update` and `lock` before they save `shulker.json` and `shulker.lock`, and by an in-place build before it writes over anything you changed. A build that only places what the lock already says takes none, because the relock that changed the lock kept that state already. It holds the manifest, the lock, the whole `config` directory and every other file the build manages; mod and pack files aren't copied, since the restored lock brings them back from the cache. Only a project with a target that builds in place keeps history. The number in front of each entry is what `history show` and `rollback` take. Alias: `ls`.

```sh
shulker history list
```

### `shulker history show`

Show one entry and what restoring it would do to the mods, resource packs and shaders it holds: what would come back, what would go, and what would change version. With no number, shows the newest.

```sh
shulker history show
shulker history show 3
```

### `shulker history prune`

Remove every entry beyond the number `history` in `shulker.json` keeps, 5 by default. Nothing else deletes history: a build over the number only warns. With `history` set to `-1` nothing is ever removed and the warning never appears; with `0` no entry is taken at all.

```sh
shulker history prune
```

### `shulker rollback`

Restore a history entry and build it in place. The current state is kept as an entry of its own first, so a rollback can itself be rolled back. With no number, restores the newest.

```sh
shulker rollback
shulker rollback 2
shulker rollback --prune
```

| Flag | Description |
| --- | --- |
| `--prune` | Also trim history to the number the manifest keeps |

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

### `shulker link atlauncher`

Create an ATLauncher instance that syncs the client build before each launch.

shulker writes the instance itself: the Minecraft version, the loader if the project has one, and a pre-launch command that runs `shulker sync`. ATLauncher downloads the game, its libraries and Java the first time you press Play. For NeoForge and Forge, shulker runs the loader's installer once per loader version and copies what it builds into ATLauncher's `libraries` folder. A new instance gets the shulker image; an image you pick in ATLauncher is kept when you link again. ATLauncher only reads its instances when it starts, so restart it if it is open.

With no source, it links the project in the current directory. Pass a project directory, git URL, or manifest URL to link that instead. shulker then syncs the instance right away, so it's ready to play, and keeps it up to date from the same source before each launch. The instance folder is named after the letters and digits in the instance name. Running `link` again keeps the settings you changed in ATLauncher, such as memory and Java arguments.

`--with` and `--without` are saved in the instance's own `shulker.local.json`. Change them later with `shulker feature on|off --into <instance folder>`, or run `link` again with new flags.

If the instance already syncs from a different source, or is an ATLauncher instance shulker didn't link, `link` fails rather than taking it over. Use `--name` to create a second instance, or `--force` to link over this one.

```sh
shulker link atlauncher
shulker link atlauncher https://github.com/shulker-sh/base-pack.git
shulker link atlauncher https://example.com/pack/shulker.json --name "Friends SMP" --with shaders
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher data directory (default: ATLauncher's) |
| `--target <name>` | Client target to link (default: the only client target) |
| `--name <name>` | Instance name (default: the target's display name) |
| `--as <id>` | Id for this instance, which `-i` takes (default: derived from its name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Link over an instance that syncs from a different source or that shulker didn't link |
| `--with <feature>` | Turn a feature on for this instance; repeat for more |
| `--without <feature>` | Turn a feature off for this instance; repeat for more |

### `shulker link gdlauncher`

Create a GDLauncher instance that syncs the client build before each launch.

shulker writes the instance's `instance.json` itself: the Minecraft version, the loader if the project has one, and a pre-launch hook that runs `shulker sync`. GDLauncher downloads the game, the loader and Java the first time you press Play, NeoForge and Forge included. Every `link` marks the instance for setup again, so the next Play re-checks the install and takes a little longer. It can only install loader versions on its own list, which trails new releases by a few days. When the locked loader version isn't on that list yet, the instance uses the newest one GDLauncher has and `link` warns you; run `link` again once GDLauncher adds it, or pass `--force` to use the locked version anyway. A new instance gets the shulker icon; linking again never changes the icon, so one you pick in GDLauncher, or the default, stays. GDLauncher only reads its instances when it starts, and while open it writes its own copy back over them when you change settings or play, so quit it before linking and open it afterwards. On macOS and Linux, `link` and `unlink` warn you when GDLauncher is open.

With no source, it links the project in the current directory. Pass a project directory, git URL, or manifest URL to link that instead. shulker then syncs the instance right away, so it's ready to play, and keeps it up to date from the same source before each launch. The instance folder is named the way GDLauncher names it. Running `link` again keeps the settings you changed in GDLauncher, such as memory and Java arguments. If you moved GDLauncher's runtime path in its settings, shulker follows it.

Renaming the instance in GDLauncher moves its folder. It keeps syncing before each launch, but `shulker instances` reports it missing; run `link` again with the new `--name`, and `shulker unlink` the old one.

`--with` and `--without` are saved in the instance's own `shulker.local.json`. Change them later with `shulker feature on|off --into <game folder>`, or run `link` again with new flags.

If the instance already syncs from a different source, or is a GDLauncher instance shulker didn't link, `link` fails rather than taking it over. Use `--name` to create a second instance, or `--force` to link over this one.

```sh
shulker link gdlauncher
shulker link gdlauncher https://github.com/shulker-sh/base-pack.git
shulker link gdlauncher https://example.com/pack/shulker.json --name "Friends SMP" --with shaders
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher runtime directory (default: GDLauncher's) |
| `--target <name>` | Client target to link (default: the only client target) |
| `--name <name>` | Instance name (default: the target's display name) |
| `--as <id>` | Id for this instance, which `-i` takes (default: derived from its name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Link over an instance that syncs from a different source or that shulker didn't link, and use the locked loader version even if GDLauncher can't install it yet |
| `--with <feature>` | Turn a feature on for this instance; repeat for more |
| `--without <feature>` | Turn a feature off for this instance; repeat for more |

### `shulker link mojang`

Install the project's loader, if it has one, into the official launcher and add a profile that points at the client build. Alias: `vanilla`.

With no source, it links the project in the current directory, and the profile's game directory is the project's `build/<target>`. Pass a project directory, git URL, or manifest URL to link that instead: the game directory is then `shulker/<slug>` inside the launcher directory, and shulker syncs it right away so it's ready to play. The official launcher has no pre-launch hook, so the profile doesn't update itself; run `shulker sync -i <id>` (or `shulker sync --all`) to bring it up to date.

If the profile already syncs from a different source, `link` fails rather than repointing it. Use `--name` to create a second profile, or `--force` to repoint this one.

```sh
shulker link mojang
shulker link mojang https://github.com/shulker-sh/base-pack.git
shulker link mojang https://example.com/pack/shulker.json --name "Friends SMP"
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher directory (default: the official launcher's `.minecraft` folder) |
| `--target <name>` | Client target to link (default: the only client target) |
| `--name <name>` | Profile name (default: the target's display name) |
| `--as <id>` | Id for this instance, which `-i` takes (default: derived from its name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Repoint a profile that syncs from a different source |

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
| `--as <id>` | Id for this instance, which `-i` takes (default: derived from its name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Repoint an instance that syncs from a different source |
| `--with <feature>` | Turn a feature on for this instance; repeat for more (sync mode only) |
| `--without <feature>` | Turn a feature off for this instance; repeat for more (sync mode only) |

### `shulker sync`

Download and build one target of a project straight into a directory, without setting up a project there. The source can be a project directory, a git URL, or a manifest URL. Worlds, logs, screenshots and crash reports stay in the directory you sync into, and nothing is written into the source project; only the project's own build directories link them to its `data/<target>/`.

```sh
shulker sync https://github.com/shulker-sh/base-pack.git --target server --into /srv/minecraft
shulker sync ../my-pack --target client --into ~/instances/my-pack
shulker sync -i friends-smp
shulker sync --into ~/instances/my-pack
shulker sync --all --side server
shulker sync
```

With `--into` and no source, shulker reads what the directory syncs from out of its own `.shulker/instance.json`, so a synced directory keeps working even if the registry is gone.

If a git or manifest URL can't be reached because the network is down, `sync` warns and builds from the copy used by the last sync from that source that succeeded, so an instance still launches offline. The warning names the commit and says how old that copy is. A server that answers with an error, a missing ref, or a failed login still fails the sync, and so does a source that has never synced successfully here. `--offline` skips the network entirely, which is quicker than waiting for timeouts on a network that drops traffic. For a server target, an installed Java runtime is kept when its update check can't reach the network.

Every directory shulker syncs into gets a `.shulker/instance.json` recording what it syncs from, and an index of those directories lives in `registry.json` beside shulker's `config.json` (a `registry` path in `config.json`, relative to that file, moves it). A `sync --into` adds the directory to that index under an id derived from its name, or the one `--as` gives it, and `link` does the same for each launcher instance or profile. Syncing into the target's own build directory adds nothing. [`shulker instances`](#shulker-instances) shows the index.

To update something on that index, name it instead of a source. `-i` takes an instance's id, its name, or its directory, and syncs it from what its instance file records. Ids are unique, so `-i <id>` always picks exactly one; a name several instances share needs `--launcher` or `--side` to narrow it, or `--all` to sync them all. `--all` alone syncs every instance. It keeps going when one fails, and exits with an error at the end. With no source and neither flag, `sync` run inside a project syncs every instance synced from that project, narrowed by `--launcher` or `--side`. Outside a project it asks which one to sync when run in a terminal, and fails with the list otherwise.

A project whose target builds into its own directory is an instance, and `sync` run inside it (or naming it with `-i`) updates the instance itself first: modpacks that follow their source are fetched again (every modpack except one set to `"autoUpdate": false`), the lock is resolved against them without moving your own mods, and the target is built in place. Nothing is written, and no history entry is taken, when the lock comes out unchanged. Every instance synced from it is synced after, since those build from its lock. A modpack update your own mods can't satisfy stops the sync with the reason, leaving the lock and the directory as they were; a launcher's pre-launch hook instead builds what the lock already has and starts the game.

| Flag | Description |
| --- | --- |
| `--target <name>` | Target to build (default: the only target) |
| `--into <path>` | Output directory (default: the target's build directory) |
| `--name <name>` | Name to list the `--into` directory under (default: the target's display name; kept on later syncs) |
| `--as <id>` | Id to list the `--into` directory under, which `-i` takes (default: derived from its name) |
| `--all` | Sync every instance `-i` matches, or every instance when there's no `-i` |
| `--launcher <launcher>` | Only instances linked in this launcher: `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--side <side>` | Only `client` or `server` instances |
| `--offline` | Don't use the network; build from the last successful sync and cached files |
| `--force` | Overwrite files edited in the output directory |
| `--ref <ref>` | Branch, tag, or commit to sync from a git source (default: the remote HEAD) |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |

### `shulker instances`

List the instances shulker keeps in sync, grouped by launcher, with plain `sync --into` directories last. Each row leads with the instance's id, which is what `-i` takes, and shows its side, when it was last synced, its directory, and the name, source and target it syncs from. A directory that is gone or can't be read is flagged, and so is one missing its `.shulker/instance.json`.

```sh
shulker instances
```

```
Prism Launcher
  friends-smp (client), synced 2026-09-11 14:02
    ~/Library/Application Support/PrismLauncher/instances/shulker-friends-smp/minecraft
    Friends SMP, from https://github.com/shulker-sh/base-pack.git, target client

Other directories
  smp-server (server), synced 2026-09-10 21:40
    /srv/minecraft
    My Pack server, from https://github.com/shulker-sh/base-pack.git, ref v3, target server
```

### `shulker instances repair`

Put the registry back in step with what is on disk. It works even when `registry.json` can't be read, rewriting it from what it finds: it scans each launcher's own instances directory, registers any folder shulker syncs that isn't in the index and wasn't unlinked, and writes a `.shulker/instance.json` for any instance missing one, from what that directory's last build recorded. A registered directory that is gone is reported rather than dropped, since an unmounted disk looks exactly like a deleted instance; [`shulker unlink`](#shulker-unlink) is what forgets one. `shulker self update` runs it after a successful update.

```sh
shulker instances repair
shulker instances repair --launcher prism
shulker instances repair --launcher prism --launcher-dir ~/other-prism
```

| Flag | Description |
| --- | --- |
| `--launcher <launcher>` | Only scan this launcher: `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--launcher-dir <path>` | Scan this directory instead of the launcher's own; needs `--launcher` |

### `shulker unlink`

Stop syncing a linked instance or synced directory and remove it from the list. Its files, worlds, and feature choices stay. For a Prism Launcher or MultiMC instance, `unlink` removes the pre-launch sync but keeps the instance. It leaves a pre-launch command alone if you replaced shulker's with your own. For the official launcher, it removes the profile but keeps the build directory and the installed loader. A plain synced directory is just forgotten. The directory's `.shulker/instance.json` is marked unlinked, so [`shulker instances repair`](#shulker-instances-repair) doesn't register it again; linking or syncing into it clears the mark. When the entry syncs from a project directory, `unlink` also drops it from that project's `shulker.local.json`, so a bare `shulker sync` there no longer builds it.

Name the instance by the id [`shulker instances`](#shulker-instances) shows, by the name its launcher shows, or by its directory. Inside a project, a launcher name (`mojang`, `prism`, `multimc`, `atlauncher`, `gdlauncher`) unlinks that project's instance in that launcher, the reverse of `shulker link <launcher>`; an instance actually called that name comes first. A name several instances share needs `--launcher`, `--side`, or `--all`, while an id always picks one. `unlink` prints the command that sets the instance up again.

```sh
shulker unlink mojang
shulker unlink "Friends SMP"
shulker unlink "My Pack" --launcher prism
shulker unlink --all --side server
```

| Flag | Description |
| --- | --- |
| `--all` | Unlink every entry the name matches, or every entry when there's no name |
| `--launcher <launcher>` | Only entries linked in this launcher: `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--side <side>` | Only `client` or `server` entries |

### `shulker hook pre-launch`

What a launcher's own pre-launch slot runs. shulker writes the script that calls it into the instance's `.shulker/` folder and points the launcher at that, so there is no reason to run this yourself: outside a launcher slot it would sync whatever directory it was run in. It syncs the instance from its source before the game starts, and a failure never stops the game — the launcher plays what is already on disk.

A launcher that gives shulker no way to show a message gets a deadline instead, so a long update can explain itself rather than looking like a hang. That is GDLauncher only, which discards a hook's output when its own five-minute limit runs out.

| Flag | Description |
| --- | --- |
| `--deadline <duration>` | Stop the update after this long and say so, aborting the launch (default: no deadline) |

### `shulker hook post-exit`

What a launcher's own post-exit slot runs, recording how the run ended in the instance's `.shulker/launches.json`: when it started and finished, whether the game left a crash report, and where that report and the log are. `settings.launchHistory` in `.shulker/instance.json` is how many runs are kept — 5 by default, `-1` every one, and `0` none at all, which records nothing.

## Types

`add`, `remove`, and `list` span every kind of thing a project requires. Each kind also has a group of its own, which is the plain verb with that `--type` and only the flags that kind takes.

### `shulker mod add|remove|list`

`shulker mod add sodium` is `shulker add sodium --type mod`, and the same for `remove` and `list`. Flags: `--side`, `--channel`, `--pin`, `--provider`, `--as`, `--with-deps`.

```sh
shulker mod add sodium
shulker mod list
```

### `shulker modpack add|remove|list`

A modpack is another shulker project whose mods and overrides merge into this one. `shulker modpack add ../base-pack` is `shulker add ../base-pack --type modpack`; the source is a local path, git URL, or raw manifest URL. `remove` prunes the mods only that modpack provided, and `list` shows each modpack's locked ref and whether a local one has changed. Flags: `--ref`, `--as`, `--unlocked`, `--no-auto-update`.

A modpack that ships a `shulker.lock` is **locked**: its exact versions, dependencies included, are copied into this project's lock and marked with the modpack they came from, and its Minecraft and loader must match this project's exactly. A modpack without a lock, or one added with `--unlocked`, is **floating**: its mods are resolved here like your own, and its Minecraft and loader only have to admit this project's versions. A mod you list in `shulker.json` yourself always wins over either. Change your mind later with `shulker set requires.<key>.locked true|false`.

```sh
shulker modpack add https://github.com/shulker-sh/base-pack.git --ref v3
shulker modpack add ../base-pack --as base
shulker modpack list
shulker modpack remove base-pack
```

### `shulker resourcepack add|remove|list`

`shulker resourcepack add fresh-animations` is `shulker add fresh-animations --type resourcepack`, and the same for `remove` and `list`. The provider's own project type decides what an entry is, so the plain `shulker add` usually needs no `--type` at all. A resource pack is placed as `resourcepacks/<key>.zip`, named by its `requires` key rather than the provider's file name, so one you enabled in game stays enabled when it updates. Flags: `--channel`, `--pin`, `--provider`, `--as`.

```sh
shulker resourcepack add fresh-animations
shulker resourcepack list
```

### `shulker shader add|remove|list`

`shulker shader add complementary-reimagined` is `shulker add complementary-reimagined --type shader`, and the same for `remove` and `list`. A shader is placed as `shaderpacks/<key>.zip` and enabled through its shader mod's own config: `config/iris.properties`, or `config/oculus.properties` on Forge. One that ships vanilla core shaders needs no shader mod at all, so it is placed in `resourcepacks/` and enabled like a resource pack. Flags: `--channel`, `--pin`, `--provider`, `--as`.

```sh
shulker shader add complementary-reimagined
shulker shader list
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

### `shulker docs`

Print the documentation built into this shulker, so it always matches the installed version and works offline. With no arguments it lists the pages. A page name prints that page, a command prints its section (`shulker docs add`), and any other heading prints its section (`shulker docs sides`). A page name followed by more words looks only inside that page (`shulker docs lock modpack`). When several sections match, it lists the command that prints each one; when none does, it searches every page for the words. Pages and sections print as markdown, which `--json` returns in `markdown`.

```sh
shulker docs
shulker docs add
shulker docs lock modpack
shulker docs --search build directory
```

| Flag | Description |
| --- | --- |
| `-s, --search` | Search every page for the words instead of looking up a page or heading |

### `shulker cache info`

Show where the shared download cache is, how much space it uses, how many files it holds, and how much `cache prune` would free. The roots line names what is keeping files: every instance in the registry, and the project you are standing in when there is one. A registered instance whose lock can't be read is named as a warning and no prune line is suggested, since `cache prune` refuses while one is unreadable; the prunable figure is then counted as if that instance needed nothing.

```sh
shulker cache info
```

### `shulker cache prune`

Remove everything in the cache that no root references. A root is a registered instance or the project you run it in: its `shulker.lock`, the lock of every history entry it keeps, and the modpack checkouts and offline sync fallbacks its sources need. Installer logs and half-finished downloads always go. The managed Java runtimes and your CurseForge key are never touched, and nothing a build placed can be removed from a directory without its bytes reaching the cache first, so rolling an instance back still works offline. A registered folder that no longer exists is skipped; one that is there but whose lock can't be read stops the prune, since it may be an instance that still needs its files.

```sh
shulker cache prune
```

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

### `shulker completion bash`

Print the bash completion script, so Tab completes shulker's commands, flags, and arguments. Load it in the current shell, or add that line to `~/.bashrc` to have it in every new shell. It needs the bash-completion package.

```sh
source <(shulker completion bash)
```

| Flag | Description |
| --- | --- |
| `--no-descriptions` | Leave command descriptions out of the completions |

### `shulker completion zsh`

Print the zsh completion script, so Tab completes shulker's commands, flags, and arguments. Load it in the current shell, or save it where zsh looks for completions to have it in every new shell. It needs `compinit`, which most zsh setups already run.

```sh
source <(shulker completion zsh)
shulker completion zsh > "${fpath[1]}/_shulker"
```

| Flag | Description |
| --- | --- |
| `--no-descriptions` | Leave command descriptions out of the completions |

### `shulker completion fish`

Print the fish completion script, so Tab completes shulker's commands, flags, and arguments. Load it in the current shell, or save it in fish's completions directory to have it in every new shell.

```sh
shulker completion fish | source
shulker completion fish > ~/.config/fish/completions/shulker.fish
```

| Flag | Description |
| --- | --- |
| `--no-descriptions` | Leave command descriptions out of the completions |

### `shulker completion powershell`

Print the PowerShell completion script, so Tab completes shulker's commands, flags, and arguments. Load it in the current session, or add that line to your PowerShell profile to have it in every new session.

```powershell
shulker completion powershell | Out-String | Invoke-Expression
```

| Flag | Description |
| --- | --- |
| `--no-descriptions` | Leave command descriptions out of the completions |

## JSON output

With `--json`, every command prints one JSON object on stdout, whether it succeeds or fails:

```json
{
  "ok": true,
  "command": "modpack add",
  "lockStale": false,
  "warnings": [],
  "data": {}
}
```

| Field | Description |
| --- | --- |
| `ok` | `true` when the command succeeded |
| `command` | The command that ran, like `modpack add` |
| `lockStale` | `shulker.lock` doesn't match `shulker.json`; `shulker lock` brings it in line. Commands that build from the lock warn, naming each difference, and carry on; `export` refuses |
| `warnings` | Everything shulker would print as a `!` line without `--json`. Always present, empty when there are none |
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

`lock`, `add`, `remove`, `update`, `pin`, `unpin`, `modpack add`, and `modpack remove` all return the same `data`: what changed in `shulker.lock` and `shulker.json`.

```json
{
  "reresolved": [],
  "platform": [],
  "added": [{ "id": "fabric-api", "versionNumber": "0.119.0", "side": "both", "provider": "modrinth", "requiredBy": ["sodium"] }],
  "updated": [{ "id": "lithium", "from": "0.14.1", "to": "0.14.3" }],
  "removed": [{ "id": "iris", "versionNumber": "1.8.0", "requiredBy": [] }],
  "modpacks": [{ "name": "base", "from": "abc1234", "to": "def5678" }],
  "suggestions": []
}
```

| Field | Description |
| --- | --- |
| `reresolved` | Why every mod was resolved again, one difference per entry, like `minecraft: locked 26.1 is outside ~26.2`. Empty when only some mods changed |
| `platform` | `minecraft` and `loader` when their locked version changed, as `{ "id", "from", "to" }`. `from` is empty for a new lock |
| `added` | Mods newly locked. `requiredBy` names the mods and modpacks that pulled one in; empty when only `shulker.json` lists it. `alreadyLocked` marks a dependency that `add` just listed in `shulker.json` |
| `updated` | Mods whose locked version, provider, side, or channel changed. `fromProvider`/`toProvider`, `fromSide`/`toSide`, and `fromChannel`/`toChannel` appear when that field changed |
| `removed` | Mods no longer locked, with the `requiredBy` they had. `stillLocked` marks a mod taken out of `shulker.json` that a modpack still provides |
| `modpacks` | Modpacks added, removed, or moved to another commit. `from` is empty for a new modpack, `to` for a removed one |
| `suggestions` | Recommended mods that aren't installed |
| `pin` | `pin` only: the version it pinned to |

### Error codes

Without `--json`, the error line ends with its code, like `✘ error: sodium is not in the manifest (mod-not-found)`, with the items and candidates in a tree underneath.

| Code | Meaning |
| --- | --- |
| `already-ignored` | The pair already has an ignore in `shulker.json`; pass `--force` to replace it |
| `ambiguous-instance` | Several instances match the name given. `candidates`: the matches, `pass`: their ids, which are unique |
| `ambiguous-into` | The target has edits in several synced directories; pass `--into`. `candidates`: the directories |
| `ambiguous-target` | Several targets fit; pass `--target`. `candidates`: the targets |
| `build-conflict` | Files changed both in the build directory and in the source; run `diff`, or pass `--force` to overwrite. `items`: the files |
| `build-reserved` | A target that builds in place has overrides that would write `shulker.json`, `shulker.lock`, `shulker.local.json`, `.shulker/` or a data directory. `items`: the files |
| `cache-root-unreadable` | A registered instance's `shulker.lock` is there but can't be read, so `cache prune` stops rather than remove files that instance may need; `cache info` still reports and names the instance |
| `config-invalid` | shulker's `config.json` isn't valid JSON; the message names the line and column. Only commands that need its registry location fail; the rest warn and go on without it |
| `curseforge-key-rejected` | CurseForge rejected the API key: your own, or shulker's built-in one when shulker.sh has no working replacement |
| `curseforge-not-found` | `export curseforge` found nothing on CurseForge for these mods, resource packs or shaders; pass `--bundle`. `items`: what is missing |
| `deps-held` | A mod being added needs another version of a dependency the lock holds; `--with-deps` moves them. `items`: each held version and what needs it |
| `registry-has-instances` | `config set` or `config unset` would move the registry away from instances the new one doesn't have; `--force` changes it anyway. `items`: the directories left behind |
| `registry-invalid` | shulker's `registry.json`, the list of linked instances and synced directories, isn't valid JSON; the message names the line and column |
| `error` | Anything unexpected, like a file that can't be read or written. The message has the details |
| `eula-required` | The server needs the Minecraft EULA accepted |
| `feature-not-found` | No mod or target uses the feature. `candidates`: the features in use |
| `file-not-found` | A file named to `pull` isn't in the build directory |
| `git-missing` | A git source needs `git` on PATH |
| `history-empty` | The instance has no history entries yet; one is taken before an in-place build changes anything |
| `history-invalid` | A history entry's own record is unreadable; `history prune` removes it |
| `history-missing` | There is no history entry with that number; the message says how many are kept |
| `installer-failed` | NeoForge's or Forge's own installer failed while setting up a server dir or a launcher; the message shows its last output and names the log in shulker's cache that holds all of it |
| `instance-dir-not-empty` | The instance directory already has files |
| `instance-exists` | An instance already syncs from a different source, or is an ATLauncher or GDLauncher instance shulker didn't link; pass `--name` for a second one, or `--force` |
| `instance-missing` | A linked instance's directory is gone |
| `source-unknown` | `sync --into` found no record in the directory of what it was synced from; name the source |
| `instance-not-found` | No instance matches. `candidates`: the instances shulker knows, `pass`: their ids |
| `instance-id-taken` | Another instance already has the `--as` id; the message names its directory |
| `instance-invalid` | An instance's `.shulker/instance.json` doesn't parse, doesn't match its schema, or names a `$schema` this shulker doesn't know; `shulker instances repair` writes it again |
| `interrupted` | Ctrl-C or SIGTERM stopped the command. Files are left whole: each one is written in full or not at all. A second Ctrl-C quits at once |
| `into-missing` | The `--into` directory does not exist |
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
| `loader-required` | `add` of a mod in a project without a loader; set one with `shulker set loader.type <loader>` |
| `loader-install-incomplete` | The loader's installer left no launcher profile to read the installed version from |
| `local-invalid` | `shulker.local.json` isn't valid JSON; the message names the line and column |
| `lock-invalid` | `shulker.lock` doesn't parse (the message names the line and column) or doesn't match its schema (one line per failing field, by dotted path), or a change would make it invalid. `items`: the failing fields when there are several |
| `lock-not-found` | No `shulker.lock`; run `shulker lock` |
| `lock-stale` | `export` needs a lock that matches `shulker.json`; run `shulker lock`. Other commands only warn. `items`: each difference |
| `manifest-exists` | A `shulker.json` is already where `init` or `import` would write one |
| `manifest-invalid` | `shulker.json` doesn't parse (the message names the line and column) or doesn't match its schema (one line per failing field, by dotted path), or a change would make it invalid. `items`: the failing fields when there are several |
| `manifest-not-found` | No `shulker.json` in the project directory or the sync source |
| `manual-download` | The provider doesn't distribute this mod; download it into `downloads/` |
| `memory` | Server memory isn't a whole number of M or G |
| `minecraft-required` | `shulker.json` sets no `minecraft` and no locked modpack supplies one; set it with `shulker set minecraft <version>` |
| `missing-files` | Mods that need a manual download are missing. `items`: what to download |
| `mod-not-found` | The mod isn't on any provider, or isn't in `shulker.json`. `candidates`: the mods in `shulker.json`, where relevant |
| `mrpack-download` | A file in the modpack couldn't be downloaded |
| `mrpack-host-not-allowed` | Modrinth launchers only download from `cdn.modrinth.com`, `github.com`, `raw.githubusercontent.com` and `gitlab.com`, so they won't download these files; pass `--bundle`. `items`: the files |
| `mrpack-invalid` | The modpack is malformed |
| `mrpack-marker` | The modpack's own `shulker.json` or `shulker.lock` can't be read, whether it came from the archive root or the marker jar |
| `mrpack-unsupported` | The modpack's format isn't supported |
| `no-compatible-version` | The mod has no version for this Minecraft and loader. `candidates`: other release channels that have one |
| `no-instances` | Nothing is linked yet |
| `no-problem` | The locked mods have no dependency problem for the pair; pass `--rule` and `--declared` from the failed command. `candidates`: the current problems, where there are any |
| `no-target` | `shulker.json` has no target of the side the command needs |
| `not-built` | The target has no build directory yet; run `shulker build` |
| `not-direct` | The mod is only a dependency. `items`: the mods that require it |
| `not-drifted` | A file named to `pull` has no changes. `candidates`: the changed files |
| `not-ignored` | The pair has no ignore in `shulker.json`. `candidates`: the pairs that do |
| `not-in-place` | The project has no target that builds in place, so it keeps no history |
| `not-installed` | A file isn't in the cache; run `shulker install` |
| `not-pinned` | The mod has no pin |
| `not-synced` | The directory has no record of the source it was synced from |
| `modpack-changed` | A modpack no longer matches the lock; run `shulker update` |
| `modpack-conflict` | Two modpacks list the same mod with different settings |
| `modpack-exists` | The modpack is already in `shulker.json` |
| `modpack-fetch` | A modpack couldn't be fetched |
| `modpack-lock-missing` | A modpack is set `locked: true` but its source has no `shulker.lock`; run `shulker lock` there, or set locked false |
| `modpack-manifest` | A modpack source has no `shulker.json` |
| `modpack-mismatch` | A modpack wants a different Minecraft version or loader |
| `modpack-name` | A modpack's name can't be worked out from its source; pass `--as` |
| `modpack-not-found` | The modpack isn't in `shulker.json`. `candidates`: the modpacks |
| `modpack-platform` | Locked modpacks disagree about Minecraft or the loader, and `shulker.json` sets neither; set `minecraft`/`loader`, or unlock one |
| `modpack-provided` | The mod comes from a modpack, so it can't be removed on its own |
| `modpack-ref` | A modpack's `ref` doesn't apply to its source, or wasn't found |
| `modpack-unlocked` | A modpack has no commit in the lock; run `shulker update` |
| `path-invalid` | `shulker.json` or `config.json` has no such field, or the path goes inside a single value or a list. `candidates`: the fields allowed there |
| `path-not-set` | `get` or `config get` names a field that isn't set |
| `pin-mismatch` | The pinned version belongs to a different project |
| `player-invalid` | Neither a player name nor a uuid |
| `player-reassigned` | Player names now belong to different accounts; pass `--accept-player-change`. `items`: the players |
| `player-unknown` | Players that don't exist at Mojang. `items`: the names |
| `player-unresolved` | A player isn't in the lock; run `shulker player` |
| `players-invalid` | A player entry in `shulker.json` is invalid |
| `properties-invalid` | `server.properties` keys removed in this Minecraft version, or values that aren't valid. Unknown keys only warn, with a did-you-mean. `items`: the problems |
| `provider-unavailable` | The provider isn't set up, like CurseForge without an API key |
| `requires-taken` | Another `requires` entry already holds the key, or the mod's jar id is already locked under another key; pass `--as <key>` |
| `requires-unsupported` | A `requires` entry is a kind shulker can't resolve yet: a local `file`, or a modpack from a provider rather than a `source` |
| `runtime-unavailable` | Mojang publishes no Java runtime for this platform; set `java` in `shulker.json` |
| `self-update-check` | Checking for a release failed, or none is published |
| `self-update-checksum` | The download doesn't match its checksum |
| `self-update-download` | The download failed |
| `self-update-install` | The running binary couldn't be replaced |
| `self-update-provenance` | `--require-attestation` is set and the build provenance couldn't be verified |
| `server-exit` | The server exited with an error. `items`: its `logs/latest.log` and, when the server wrote one during the run, its crash report; `data` carries them as `log` and `crashReport` |
| `source-fetch` | The sync source couldn't be fetched |
| `source-lock` | The sync source has no `shulker.lock` |
| `source-offline` | Offline, and the source has never synced here, so there's no copy to use |
| `source-ref` | `--ref` doesn't apply to the source, or wasn't found |
| `sync-failed` | Some entries failed to sync; `data` has each entry's result |
| `target-exists` | The target is already in `shulker.json` |
| `target-not-found` | No such target. `candidates`: the targets |
| `topic-not-found` | `docs` found no page, heading or line matching the words. `candidates`: the pages |
| `type-ambiguous` | A CurseForge slug matches projects of several types; pass `--type` to choose. `candidates`: the types it matched |
| `type-mismatch` | `--type` disagrees with what the provider says the project is. `candidates`: the provider's own type |
| `unlink-failed` | Some entries couldn't be unlinked; `data` has each entry's result |
| `unset-variable` | An override uses a variable that isn't set |
| `unsupported-loader` | shulker doesn't support the loader yet |
| `unsupported-mode` | `--mode symlink` isn't supported on Windows yet |
| `update-paused` | The pre-launch hook stopped a GDLauncher update at four minutes so it could explain itself; the launch is aborted, and launching again resumes it. Shown in GDLauncher's own dialog, so it prints without shulker's usual error decoration |
| `usage` | An unknown command or flag, wrong arguments, or a flag value that isn't allowed. `items`: the missing or unexpected arguments, when that's the problem. Exits 2 |
| `validation-failed` | The locked mods have dependency problems; each prints the `shulker ignore` command that would accept it. `items`: the problems |
| `version-not-found` | The provider has no version with the id given to `add --pin` or `pin`; the message links the mod's versions page |
| `version-required` | `export mrpack` and `export curseforge` need a version |
| `wrong-side-target` | The target is on the wrong side for the command. `candidates`: the targets on the right side |
