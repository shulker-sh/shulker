---
description: Every shulker command with its flags and examples.
outline: [2, 3]
---

# CLI reference

| Command | Description |
| --- | --- |
| [`shulker init`](#shulker-init) | Create shulker.json and a lock in the current directory |
| [`shulker add <mod>...`](#shulker-add) | Add mods to the manifest and lock |
| [`shulker search <words>...`](#shulker-search) | Search the providers for projects to add |
| [`shulker remove <mod>...`](#shulker-remove) | Remove mods from the manifest and lock |
| [`shulker lock`](#shulker-lock) | Bring the lock in line with shulker.json without upgrading |
| [`shulker update [mod...]`](#shulker-update) | Update mods to the newest compatible version |
| [`shulker outdated [mod...]`](#shulker-outdated) | Show mods with a newer compatible version |
| [`shulker suggests`](#shulker-suggests) | List mods that locked mods recommend and that aren't installed |
| [`shulker pin <mod> [version]`](#shulker-pin) | Pin a mod to a provider version id |
| [`shulker unpin <mod>`](#shulker-unpin) | Remove a mod's pin and re-resolve it |
| [`shulker ignore <mod> <on>`](#shulker-ignore) | Record that a dependency problem is safe to ignore |
| [`shulker unignore <mod> <on>`](#shulker-unignore) | Drop an ignored dependency problem |
| [`shulker set <path> <value>`](#shulker-set) | Set a field in shulker.json |
| [`shulker unset <path>`](#shulker-unset) | Remove a field from shulker.json |
| [`shulker get [path]`](#shulker-get) | Print a field of shulker.json, or all of it |
| [`shulker feature on\|off <feature>`](#shulker-feature-on-off) | Turn a feature on or off on this machine |
| [`shulker feature reset <feature>`](#shulker-feature-reset) | Go back to the declared default for a feature |
| [`shulker feature list`](#shulker-feature-list) | List features and whether they're on |
| [`shulker install`](#shulker-install) | Download everything in the lock and build every side |
| [`shulker build [side]`](#shulker-build) | Assemble build directories from the lock and overrides |
| [`shulker diff [side]`](#shulker-diff) | Show build files that differ from what build would write |
| [`shulker pull [file...]`](#shulker-pull) | Copy edits made in a build directory back into their source |
| [`shulker history list`](#shulker-history-list) | List the states kept before in-place builds |
| [`shulker history show [n]`](#shulker-history-show) | Show a history entry and what restoring it would change |
| [`shulker history prune`](#shulker-history-prune) | Remove history entries beyond the number the manifest keeps |
| [`shulker rollback [n]`](#shulker-rollback) | Restore a history entry and build it in place |
| [`shulker play [nickname]`](#shulker-play) | Start a shulker instance |
| [`shulker serve`](#shulker-serve) | Build the server side and run it in the foreground |
| [`shulker link shulker [source]`](#shulker-link-shulker) | Create an instance shulker owns and launches itself |
| [`shulker link atlauncher [source]`](#shulker-link-atlauncher) | Create an ATLauncher instance for the client build |
| [`shulker link gdlauncher [source]`](#shulker-link-gdlauncher) | Create a GDLauncher instance for the client build |
| [`shulker link mojang [source]`](#shulker-link-mojang) | Add a profile for the client build to the official launcher |
| [`shulker link prism [source]`](#shulker-link-prism) | Create a Prism Launcher instance for the client build |
| [`shulker link multimc [source]`](#shulker-link-multimc) | Create a MultiMC instance for the client build |
| [`shulker sync [source]`](#shulker-sync) | Download and build one side of a project into a directory, or update a linked one |
| [`shulker instances`](#shulker-instances) | List the instances shulker keeps in sync |
| [`shulker instances repair`](#shulker-instances-repair) | Register instances shulker has lost track of and write any missing instance files |
| [`shulker unlink <name>`](#shulker-unlink) | Stop syncing a linked instance or synced directory, keeping its files |
| [`shulker list`](#shulker-list) | List everything in `requires` with its locked version |
| [`shulker mod add\|remove\|list`](#shulker-mod-add-remove-list) | The plain verbs with `--type mod` |
| [`shulker modpack add\|remove\|list`](#shulker-modpack-add-remove-list) | Manage modpacks whose mods and overrides merge into this project |
| [`shulker resourcepack add\|remove\|list`](#shulker-resourcepack-add-remove-list) | The plain verbs with `--type resourcepack` |
| [`shulker shader add\|remove\|list`](#shulker-shader-add-remove-list) | The plain verbs with `--type shader` |
| [`shulker player [name\|uuid]...`](#shulker-player) | Check player names and uuids against Mojang and the lock |
| [`shulker accounts`](#shulker-accounts) | List the accounts shulker can play with |
| [`shulker accounts login`](#shulker-accounts-login) | Sign in to a Microsoft account |
| [`shulker accounts logout [name]`](#shulker-accounts-logout) | Sign a Microsoft account out |
| [`shulker accounts add <name>`](#shulker-accounts-add) | Create an offline account |
| [`shulker accounts remove <name>`](#shulker-accounts-remove) | Delete an offline account |
| [`shulker accounts refresh [name...]`](#shulker-accounts-refresh) | Renew the accounts shulker signed in |
| [`shulker accounts use <name>`](#shulker-accounts-use) | Switch the default account |
| [`shulker accounts providers`](#shulker-accounts-providers) | List the launchers shulker reads accounts from |
| [`shulker accounts providers add\|remove <launcher>`](#shulker-accounts-providers-add-remove) | Read accounts from another launcher, or stop |
| [`shulker accounts providers set <launcher...>`](#shulker-accounts-providers-set) | Replace the list, in the order given |
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
| `--no-input` | Ask nothing: every prompt takes its default, and a required value left unset is a usage error naming the flag that supplies it. Output that isn't going to a terminal implies it, and so does `--json` |
| `--no-color` | Print without colour. Setting `NO_COLOR` or `TERM=dumb` does the same, and colour is off whenever the output is not a terminal |
| `--ascii` | Print with ASCII glyphs (`*`, `x`, `|-`, `->`, `>>`) in place of `✔`, `✘`, `├─`, `⟶`, and `»` |

## Projects

### `shulker init`

Create `shulker.json` and `shulker.lock` in the current directory. On a terminal it asks six questions, in order: what you are making, which Minecraft version, whether to add mods, which mod loader, which version of it, and whether to start from an existing pack. Each question is skipped by the flag that answers it, and every answer starts on the default that flag has, so taking all six as they come creates what `--yes` creates. Answering *from an existing pack* asks for a source and adds it as a modpack, exactly as [`shulker add <source> --type modpack`](#shulker-add) would.

Under [`--no-input`](#global-flags) — which a script gets without asking for it, since output that isn't going to a terminal implies it — nothing is asked and every answer is its default: the latest release, no loader, the client side, and no pack.

```sh
shulker init
shulker init --yes
shulker init --name my-server --minecraft 1.21.1 --loader neoforge --side server
```

| Flag | Description |
| --- | --- |
| `-y, --yes` | Accept the defaults without asking: latest release, no loader, client side. On this command it is an alias of [`--no-input`](#global-flags), which leaves it nothing else to mean |
| `--name <name>` | Project name (default: directory name) |
| `--minecraft <version>` | Minecraft version or range (default: latest release) |
| `--loader <loader>` | Mod loader: `none` (the default, vanilla Minecraft), `fabric`, `quilt`, `neoforge`, `forge` |
| `--loader-version <range>` | Loader version range (default: `*`); needs `--loader` |
| `--side <side>` | Side to declare: `client` or `server` |

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
shulker export mrpack --side client -o dist/my-pack.mrpack
shulker export mrpack https://github.com/me/my-pack.git --ref v1.0
```

| Flag | Description |
| --- | --- |
| `--version <version>` | Version written into the modpack (default: `version` in shulker.json) |
| `-o, --output <path>` | Archive path (default: `build/<name>-<version>.mrpack`, or the current directory for a git or URL source) |
| `--side <side>` | Export one side only, for a deliberately partial pack (default: every declared side) |
| `--os <os>` | Include mods gated on this OS: `macos`, `windows`, or `linux` (default: leave them out) |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |
| `--bundle` | Put files that Modrinth launchers can't download inside the archive |
| `--assume-client` | Export a client even when the source declares none, from the mods and overrides both sides share |
| `--ref <ref>` | Branch, tag, or commit to export from a git source (default: the remote HEAD) |

### `shulker export curseforge`

Export the client side as a CurseForge profile `.zip` for the CurseForge app's Import Profile. Mods, resource packs and shaders locked from CurseForge go in by file ID. Everything else is looked up on CurseForge by its fingerprint, and matches go in by file ID too. Files that aren't on CurseForge fail the export unless `--bundle` ships them inside the archive, which the CurseForge app warns about on import. Server-only mods and files are left out. The profile gets shulker's logo as its image, and the archive carries `shulker.json` and `shulker.lock` at its root. The source works as in [`export mrpack`](#shulker-export-mrpack).

```sh
shulker export curseforge
shulker export curseforge --bundle -o dist/my-pack.zip
```

| Flag | Description |
| --- | --- |
| `--version <version>` | Version written into the modpack (default: `version` in shulker.json) |
| `-o, --output <path>` | Archive path (default: `build/<name>-<version>.zip`, or the current directory for a git or URL source) |
| `--os <os>` | Include mods gated on this OS: `macos`, `windows`, or `linux` (default: leave them out) |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |
| `--bundle` | Put mods that aren't on CurseForge inside the archive, and bundle every mod not from CurseForge when the lookup can't run |
| `--assume-client` | Export a client even when the source declares none, from the mods and overrides both sides share |
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
| `--as <key>` | Key used in `requires`, messages, and `requiredBy` (default: a mod's jar id, the name in a modpack's manifest) |
| `--unlocked` | Resolve a modpack's mods here instead of copying the versions its lock pins |
| `--no-auto-update` | Keep a modpack at its locked version on `shulker sync`; `shulker update` still moves it |
| `--with-deps` | Move dependency versions the lock holds when a mod being added needs another. One a locked modpack pins is listed in `shulker.json` as it moves, so it no longer follows the modpack |

### `shulker search`

Search every provider shulker has set up for projects matching the words, and print the ids to add them by. Results come in a block per provider, most downloaded first, and the command writes nothing: `shulker.json` and the lock only change through `add`. Projects CurseForge classes as something shulker has no entry type for, worlds and plugins among them, are left out. A `modpack` row is a provider modpack, which `add` can't take yet: add a modpack by source instead.

```sh
shulker search sodium
shulker search fresh animations --type resourcepack
shulker search jei --provider curseforge --limit 5
```

| Flag | Description |
| --- | --- |
| `--type <type>` | Only projects of one type: `mod`, `modpack`, `resourcepack`, `shader` |
| `--provider <provider>` | Search one provider instead of every available one: `modrinth` or `curseforge` |
| `--limit <n>` | Results to print per provider (default 10, as many as each provider answers with: at most 100 from Modrinth, 50 from CurseForge) |

With `--json`, `data.results` lists each hit as `{ "provider", "id", "slug", "title", "type", "side", "downloads" }`, and `data.query` is the words as one string.

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

Re-resolve mods to the newest compatible versions. With no arguments, fetches every modpack again, whatever its `autoUpdate`, and updates every mod; naming a modpack updates it and its mods. In an instance (a project whose side builds into its own directory), `update` then builds that side in place; elsewhere it only writes the lock and `shulker install` builds it. Alias: `upgrade`.

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

`config get`, `config set`, and `config unset` read and change shulker's own `config.json`, which applies to every project. It lives in your user config directory, or wherever `SHULKER_CONFIG` points. Three of its keys name a directory under shulker's data directory — `~/Library/Application Support/shulker` on macOS, `%AppData%\shulker` on Windows, `~/.local/share/shulker` on Linux, or wherever `SHULKER_DATA` points. Each of the four path keys takes an absolute path, or one relative to the directory holding `config.json`:

| Key | Description |
| --- | --- |
| `accounts.providers` | Where accounts are read from, in order, as a JSON array of `shulker`, `prism` or `mojang`. Without it, `["shulker"]`. An account in several providers is counted once, and the earliest one wins |
| `accounts.default` | The id of the account a launch uses when nothing else names one. [`shulker accounts use`](#shulker-accounts-use) sets it |
| `play.memory` | The heap size [`shulker play`](#shulker-play) gives the game, like `6G`. An instance's own `memory` wins over it, as each `play.` key's instance setting does; see [`shulker instance`](#shulker-instance). Without either, the JVM picks its own |
| `play.jvmArgs` | Extra JVM arguments for those launches, as a JSON array |
| `play.java` | The java those launches run: the absolute path of a java binary, or of a Java home. Without it, shulker's managed runtime |
| `play.window` | The window size those launches open at, like `1280x720` |
| `play.wrapper` | A command those launches run through, as a JSON array like `["gamemoderun"]` |
| `play.saveBackups` | How many automatic backups of a save group or instance's worlds to keep, taken before `update` or `sync` changes the mods. Without it, 5; `0` takes none. No instance setting overrides it |
| `curseforge.key` | Your CurseForge API key. `SHULKER_CURSEFORGE_KEY` takes priority when it is set |
| `registry` | The file listing linked instances and synced directories: absolute, or relative to the directory holding `config.json`. Without it, `registry.json` beside `config.json` |
| `instances` | Where [`shulker link shulker`](#shulker-link-shulker) puts the instances shulker owns. Without it, `instances` in shulker's data directory |
| `saves` | Where the save groups those instances share live. Without it, `saves` in the same data directory |
| `store` | Where a launch assembles its shared versions, libraries and assets. Without it, `game` in shulker's cache, since it holds only what shulker can fetch again |

The CurseForge key is always shown as its last four characters, like `••••c123`, unless you pass `config get --reveal`. With `--json`, `config set` and `config unset` return `{ "path", "from", "to" }` like `set`, plus `created` when they made a new registry file.

### `shulker config get`

Print a key: a string as it is, anything else as JSON. With no key, print all of `config.json`. `registry`, `instances`, `saves` and `store` show the path shulker actually uses, even when the key isn't set, and `accounts.providers` and `play.saveBackups` show their defaults the same way. A `curseforge.key` that isn't set fails with `path-not-set`.

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
shulker config set accounts.providers --literal '["shulker","prism"]'
```

A key that holds a list needs `--literal`, which reads the value as JSON. `accounts.providers` is checked as it is set: it must be a non-empty array of known provider names with no repeats, so a typo fails here rather than on the next run. The `play.` keys are checked the same way, against the rules of the instance setting of the same name.

| Flag | Description |
| --- | --- |
| `--force` | Change the registry even if it leaves linked instances or synced directories behind |
| `--literal` | Parse the value as JSON, for a list or an object |

### `shulker config unset`

Remove a key. Without `registry`, shulker goes back to `registry.json` beside `config.json`, created when missing, with the same check as `set`. Removing a key that isn't set succeeds and says so.

```sh
shulker config unset curseforge.key
```

| Flag | Description |
| --- | --- |
| `--force` | Change the registry even if it leaves linked instances or synced directories behind |

## Features

A feature is a name that mods opt into with a `feature` condition, like `shaders`. `features` in `shulker.json` declares each one, and its `default` turns it on. Your own choices are saved in `shulker.local.json` next to `shulker.json`. That file is per machine and is added to `.gitignore`. `build`, `install`, `sync`, `export mrpack`, and `export curseforge` use your choices over the declared defaults, and their `--with` and `--without` flags override both for one run.

A directory you sync into, such as a launcher instance, can have its own choices in its own `shulker.local.json`. Set them with `--into <dir>`, or with `-i <id>` for anything [`shulker instances`](#shulker-instances) lists. When you sync into it, its choices beat the project's, and `--with` and `--without` still beat both.

### `shulker feature on|off`

Turn a feature on or off for every side on this machine. It takes effect on the next build or sync, including a launcher's pre-launch sync. Naming a feature nothing in `shulker.json` uses is an error.

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

Forget your choice for a feature so it follows its declared default again.

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

Download everything in the lock and build every side the manifest declares. Run this after cloning a project.

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

Assemble a side's build directory from the lock and its overrides. With no side, builds every side the manifest declares.

```sh
shulker build
shulker build client
```

| Flag | Description |
| --- | --- |
| `--force` | Overwrite files edited in the build directory |
| `--accept-player-change` | Relock a player name that now belongs to a different account |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |

### `shulker diff`

Show what was edited in a build directory since `build` wrote it, such as config changed in-game, as a diff from the project to the directory. `build` leaves these files alone and `pull` copies the edits back. A per-key file shows only its managed keys. With no side, checks every side the manifest declares.

```sh
shulker diff
shulker diff client
shulker diff server --into /srv/minecraft
```

| Flag | Description |
| --- | --- |
| `--into <path>` | Directory the side was synced into (default: the build directory and every directory `sync` recorded) |

### `shulker pull`

Copy edits made in a build directory back into their source, an override file or keys in shulker.json, so the next build keeps them. With no files, pulls every changed file. Paths are relative to the build directory. A file some override folder already holds is updated there; a file no folder holds yet goes to `overrides/`, and `--to` names another folder instead. For a `.properties` override, only the keys it lists are pulled; name more with `--key` to start managing them.

```sh
shulker pull
shulker pull config/sodium-options.json --side client
shulker pull config/iris.properties --key colorSpace
```

| Flag | Description |
| --- | --- |
| `--side <side>` | Side whose build directory to pull from (default: the only declared side) |
| `--into <path>` | Directory the side was synced into (default: the build directory and every directory `sync` recorded) |
| `--key <key>` | Start managing this key of the one named `.properties` file, copying its current value into the override; repeat for more |
| `--to <side\|feature>` | Override folder to write into: `client` or `server` for that side's folder, or a feature name for its folder (default: where the file already lives, or `overrides/` for a new one) |

### `shulker history list`

List the states an instance kept before it changed, newest first. An entry is taken before anything is rewritten: by `add`, `remove`, `update` and `lock` before they save `shulker.json` and `shulker.lock`, and by an in-place build before it writes over anything you changed. A build that only places what the lock already says takes none, because the relock that changed the lock kept that state already. It holds the manifest, the lock, the whole `config` directory and every other file the build manages; mod and pack files aren't copied, since the restored lock brings them back from the cache. Only a project with a side that builds in place keeps history. The number in front of each entry is what `history show` and `rollback` take. Alias: `ls`.

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

## Accounts and sign-in

### `shulker accounts`

List every account shulker can see, grouped by where it came from: **Own** for the ones signed in through Microsoft, **Offline** for the ones `accounts add` created, and **Borrowed** for the ones read from another launcher. Each row carries the account's id and its state, and the default account's row is marked `✔` instead of `•`.

The id is a player UUID, or an Xbox user id for an account that owns no Java profile and so has no UUID. It is on every row because it is what tells two accounts with the same name apart, and what you type to pick one.

```sh
shulker accounts
```

The states are `playable`, `not playable (no Java profile)`, `sign-in expired`, `token expired <ago>` for a borrowed account whose launcher has not renewed it, and `offline`.

Which accounts are read comes from `accounts.providers`. A provider shulker has no reader for yet warns and contributes nothing. With `--json`, each row is `{ "id", "name", "source", "group", "state", "default" }`.

### `shulker accounts login`

Sign in to a Microsoft account and store it. Shulker prints a page to open and a code to type there; the sign-in finishes in the browser, and the command waits until it does. Running it again signs in another account, and signing in as an account that is already there updates that entry rather than adding a second.

```sh
shulker accounts login
shulker accounts login --use
```

Shulker asks for `XboxLive.signin` and `offline_access`, and nothing else: an email address would cost every player a consent screen to name the rare account that owns no Java profile, and the Xbox gamertag names that one for free. What it keeps in `accounts.json` is the Microsoft refresh token, the Minecraft session token and when it expires, the Xbox token while it lasts, and the profile — or the gamertag and Xbox user id when there is no profile. No token is ever printed or logged.

An account that owns no Java Edition is stored as `not playable`, named by its gamertag, and can't launch or be the default account; Java Edition is at minecraft.net. The sign-in leaves the default account alone and prints the [`shulker accounts use`](#shulker-accounts-use) line that switches it.

| Flag | Description |
| --- | --- |
| `--use` | Make it the default account straight away |

### `shulker accounts logout`

Sign a Microsoft account out: its entry and every token in it are deleted. With no name it is the default account that signs out.

```sh
shulker accounts logout
shulker accounts logout Notch
shulker accounts logout Notch --yes
```

Shulker asks before it signs anything out, and `--yes` answers the question ahead of time — which a run that isn't on a terminal has to pass, since there is nobody to ask. Signing out the default account leaves the one account a launch could still use behind it as the default — offline or borrowed as readily as signed in — and no default at all when there isn't exactly one to take over.

An offline account has no sign-in to end, so `logout` on one is an error naming [`shulker accounts remove`](#shulker-accounts-remove); a borrowed account belongs to the launcher it came from, and only that launcher can sign it out.

| Flag | Description |
| --- | --- |
| `-y, --yes` | Sign out without being asked first |

### `shulker accounts add`

Create an offline account: a name and a UUID shulker keeps in `accounts.json`, with no sign-in behind it. It plays singleplayer, a LAN world a mod has opened in offline mode, and a server running `online-mode=false`; an online server and Realms reject it, and so does an unmodded LAN world, which authenticates every guest against Mojang.

```sh
shulker accounts add Steve
shulker accounts add Steve --use
shulker accounts add Steve --uuid 8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21
```

The UUID is Java's type 3 UUID over `OfflinePlayer:<name>`, which is the player an offline-mode host derives for that name by itself. `--uuid` pins another, and promises little: it sets this client's own identity — the player data in a singleplayer or LAN-host save, and the id the client claims at login — and no vanilla server keys a player by it. Its use is opening a world whose player data was written under another launcher's UUID scheme. Shulker never says the pinned UUID differs from the derived one, because that difference is the point of passing it.

Names are held to the pattern a Minecraft username matches, three to sixteen letters, digits or underscores; `--allow-invalid-name` takes any other. Two offline accounts may share a name if `--force` says so, but never a UUID, since the UUID is what shulker's own file is keyed by — so a second account with the same name needs `--uuid` as well.

Creating one needs an account in sight that owns Minecraft: Java Edition, own or borrowed. It is a statement of intent rather than a licence check, and it is checked only here: afterwards the offline account plays on, and stays the default, with every Microsoft account signed out.

The default account is left alone, and the [`shulker accounts use`](#shulker-accounts-use) line that switches it is printed; `--use` switches at once.

| Flag | Description |
| --- | --- |
| `--uuid <uuid>` | Play under this UUID instead of the one the name derives |
| `--allow-invalid-name` | Take a name no Minecraft account could have |
| `--force` | Create it even though an account already answers to that name |
| `--use` | Make it the default account straight away |

### `shulker accounts remove`

Delete an offline account.

```sh
shulker accounts remove Steve
shulker accounts remove Steve --yes
```

Shulker asks first, and `--yes` answers the question ahead of time — which a run that isn't on a terminal has to pass, since there is nobody to ask. Removing the default account reseats it the way [`shulker accounts logout`](#shulker-accounts-logout) does.

With no account in sight that owns Java Edition, removing one is refused with `ownership-unproven`, since the same gate would block creating it again; `--force` removes it anyway. A Microsoft account is signed out rather than deleted, so `remove` on one is an error naming [`shulker accounts logout`](#shulker-accounts-logout), and a borrowed account belongs to the launcher it came from.

| Flag | Description |
| --- | --- |
| `-y, --yes` | Remove it without being asked first |
| `--force` | Remove it with no account in sight that could create it again |

### `shulker accounts refresh`

Renew the accounts shulker signed in itself, from the tokens it holds. Nothing is asked: the sign-in chain runs again in the background, and the Microsoft refresh token it rotates replaces the one used, since Microsoft leaves the old one working. A launch renews the account it uses on its own, so this is for renewing them ahead of time, or for picking up a profile an account has bought since.

```sh
shulker accounts refresh
shulker accounts refresh Notch
shulker accounts refresh --missing-profile
```

With no names every account of shulker's own is renewed. Names and `--missing-profile` are an AND: together they mean the named accounts that own no Java profile. An account whose sign-in has expired warns and is skipped, and the rest are still renewed; with `--json` the accounts that were renewed come back as the rows [`shulker accounts`](#shulker-accounts) prints.

| Flag | Description |
| --- | --- |
| `--missing-profile` | Only the accounts that own no Java profile |

### `shulker accounts use`

Switch the default account — the one a launch uses when nothing else names one. It is recorded as `accounts.default` in `config.json`.

```sh
shulker accounts use Notch
shulker accounts use Notch@offline
shulker accounts use 069a79f4-44e9-4726-a5be-fca90e38aaf5
```

An account is named by its username, matched without regard to case; by `name@source`, where the source is `shulker`, `offline`, `prism` or `mojang`; or by its id, dashed or not. A name may hold spaces, because an account with no Java profile is named by its Xbox gamertag, so quote it. When several accounts match, shulker asks which one on a terminal, and fails with `ambiguous-account` anywhere else, listing each match with its qualifier and its id. An account that owns no Java profile can't launch anything, so it is refused with `account-not-playable`.

### `shulker accounts providers`

List the launchers shulker reads accounts from, in the order it reads them. The list is `accounts.providers` in `config.json`; without it, shulker reads only its own accounts.

```sh
shulker accounts providers
```

The order is what settles a Microsoft account signed in to more than one launcher: it is listed once, from the earliest provider that has it. A provider shulker has no reader for yet is shown as such, and contributes nothing. With `--json`, the list comes back as an array of names.

Where a launcher's accounts are read is the directory a registered instance of it was linked against — the one named with [`shulker link prism --launcher-dir`](#shulker-link-prism) — and the launcher's usual directory on this machine otherwise. Prism's accounts come from `accounts.json` there, Microsoft and offline accounts alike. The Minecraft Launcher's come from both `launcher_accounts.json` and `launcher_accounts_microsoft_store.json` in [its own directory](#shulker-link-mojang), because that suffix is per file rather than per install; an account in both is listed once, with the session that lasts longer. Shulker opens neither the entitlements file beside them nor the launcher's stored credentials: an account's Java profile is its own proof that it owns the game, and an account with no profile isn't listed at all, since its username and UUID both live there. Shulker never renews a borrowed account and never writes to another launcher's files: a borrowed session token that has run out is shown as `token expired <ago>`, still plays, and warns at launch that online servers and Realms will reject it until that launcher renews it. A file shulker can't read warns, naming itself, and is skipped, so a corrupt one can't take the account list down — the other accounts file in the same directory still loads.

### `shulker accounts providers add|remove`

Add a launcher to the list, or take one out.

```sh
shulker accounts providers add prism
shulker accounts providers remove shulker
```

Both print the list before and after. A launcher shulker has no name for, one already in the list, one that isn't in it, and a change that would leave the list empty are all usage errors — unset `accounts.providers` to go back to the default instead of emptying it. Adding a launcher that isn't installed warns once, naming the directory that was checked; it is not an error, and nothing says it again afterwards.

### `shulker accounts providers set`

Replace the whole list, in the order given.

```sh
shulker accounts providers set shulker prism
shulker accounts providers set prism
```

The same checks apply, and a launcher new to the list warns about a missing directory the way `add` does. The generic path is `shulker config set accounts.providers --literal '["shulker","prism"]'`, which checks the value the same way.

## Running

### `shulker play`

Start a shulker instance. With no nickname it plays the instance the current directory is; `-i` names one from anywhere.

It updates the instance first, resolves the account, fetches whatever the store is missing, and then starts the game **detached**: `play` returns as soon as the game is running, and the game outlives the shell it was started from. `--no-sync` starts what is already on disk without updating it.

A detached game is still recorded. `play` hands it to a watcher — shulker itself, started again in the background with no window — which starts the game, waits for it, writes how the run ended to the instance's launch history, and exits. The record is the same one a launcher's [post-exit hook](#shulker-hook-post-exit) writes, so `instances` and the history read it the same way, except that shulker started the game itself and so also knows the status it exited with: a non-zero status is `crashed` whether or not the game managed to write a crash report. If the watcher is killed while the game is running, the record stays open with the game's process id in it, and the next command that touches the instance — `play`, `sync`, `instances repair` — closes it from the crash reports once that process has gone.

`--wait` keeps the launch in the foreground instead: `play` starts the game itself, waits for it, records the run and says how it went. `--stream` does the same and shows the game's output as it runs, on stdout, or on stderr under `--json`. The log is written either way. A game that crashes is reported, not raised: `play` exits 0, because it got the game running, which is its job.

Only the instances shulker owns can be played here: every other launcher starts its own, so an instance linked into one fails with `not-shulker`.

```sh
shulker play
shulker play smp
shulker play --account Notch
shulker play --no-sync
shulker play --wait
shulker play --stream
```

The game gets no terminal, so everything it writes goes to `.shulker/logs/<time>.log` inside the instance, one file per launch, whether or not anyone is watching. The game's own arguments carry a session access token, so they are printed nowhere: not in the log, not in a progress line, not in an error.

Who plays is the instance's pinned `account` when it has one, and otherwise the default account; `--account` names another for a single run, matched the way [`shulker accounts use`](#shulker-accounts-use) matches one. [`shulker instance set account`](#shulker-instance-set) pins one. A pinned account that has since been removed fails with `account-not-found` rather than playing as someone else. With no default account shulker takes the only account that could play and makes it the default, saying so; with several it asks on a terminal and records the answer, and off one it is a usage error naming `--account`. With no account at all it is `no-accounts`. An account whose sign-in has expired refuses the launch with the line that fixes it, and one playing on a token shulker couldn't renew — a borrowed one its launcher has let run out, or a cached one with no network — launches with a warning that online servers and Realms will reject the session.

The launch takes its memory, extra JVM arguments, Java, window size and wrapper from the instance's settings, and each one the instance leaves out from its `play.` default in `config.json` — see [`shulker instance`](#shulker-instance). `memory` becomes `-Xms` and `-Xmx`, and it and `jvmArgs` go after the version's own JVM arguments, so they win over them. The window becomes `--width` and `--height`, which the game takes for the run and never writes back; `--window` sets it for one run over both, and nothing is saved. Fullscreen isn't a launch setting: the game keeps it in `options.txt`, which the manifest's `client.options` owns. A wrapper runs the launch as its own command, with java and its arguments after the wrapper's.

`--dry-run` assembles the launch and prints it instead of starting the game: the version it resolved and what it inherits from, the libraries a loader's version brings on top of that one as a count and a size, the Java it would use, the game and natives directories, the asset index, and the classpath as a count and a size. A second run downloads nothing. It needs no account, which is what makes the plan checkable on its own.

Everything a launch shares lives in the store: `versions/`, `libraries/` and `assets/` under the store root, laid out the way the Mojang launcher lays out its own directory. Move it with `shulker config set store <path>`. A Fabric or Quilt pack's loader comes from the loader's own metadata. A NeoForge or Forge pack runs the loader's installer against the store once per loader version, after shulker has fetched the vanilla client jar the installer patches. The natives a launch unpacks are per-instance and sit in `.shulker/natives`.

```sh
shulker play --dry-run
shulker play smp --dry-run
```

| Flag | Description |
| --- | --- |
| `--account <name>` | Play as this account, for this run only (default: the instance's pinned account, else the default account) |
| `--window <w>x<h>` | Open the game at this size for this run only, like `1280x720` (default: the `window` setting) |
| `--no-sync` | Start the game without updating the instance first |
| `--wait` | Wait for the game and record how the run ended before returning |
| `--stream` | Wait for the game and show its output as it runs; the log is still written |
| `--dry-run` | Assemble the launch and print it instead of starting the game |

With `--json`, the data is `{ "instance", "version", "account", "pid", "gameDir", "log", "outcome", "exitCode", "crashReport", "sync" }`, where `account` is the row [`shulker accounts`](#shulker-accounts) prints, `pid` is the game's own process, and `sync` is absent under `--no-sync`. `outcome` (`ok` or `crashed`), `exitCode` and `crashReport` are there only under `--wait` or `--stream`, since a detached launch returns while the game is still running; `exitCode` is absent when it is 0, and `crashReport` when the game wrote none. Under `--dry-run` it is `{ "instance", "version", "inherits", "mainClass", "java", "gameDir", "nativesDir", "assetIndex", "classpath", "classpathBytes", "loaderLibraries", "loaderLibrariesBytes" }` instead, where the two `loaderLibraries` keys are absent for a version that inherits from nothing.

### `shulker serve`

Build the server side and run it in the foreground. It downloads whatever the lock needs first, the way `install` does, so a fresh clone reaches a running server in one command.

```sh
shulker serve
shulker serve server --accept-eula
```

| Flag | Description |
| --- | --- |
| `--force` | Overwrite files edited in the build directory |
| `--accept-eula` | Record acceptance of the Minecraft EULA in shulker.json without prompting |

### `shulker link shulker`

Create an instance shulker owns, under its own instances root, and register it like any other launcher's. No launcher is involved: shulker is the launcher here, so it runs the pre-launch sync and the post-exit record in process, writes no hook scripts, and fills no command slot.

With no source, it links the project in the current directory. Pass a project directory, git URL, or manifest URL to link that instead. The instance is a project of its own: a `shulker.json` that follows the source as a modpack and builds where it stands, so `shulker add` in the instance directory puts a mod on top of the pack and keeps it across syncs. shulker builds it before `link` returns.

The instance's nickname names both the folder under the instances root and the id `-i` takes, so the two can never drift. Without `--as` it comes from the pack's name. `link` never writes over a `shulker.json` that is already in the instance directory: it adopts that project, clears the unlinked mark, re-registers it and builds it where it stands, so relinking an instance you unlinked picks it back up with everything you added on top of the pack. If an instance of that name already follows a different modpack, `link` fails rather than repointing it: pass `--as` to name a second instance, or `--force` to repoint the modpack it follows.

Move the instances root with `shulker config set instances <path>`; `shulker config get instances` prints where it is now.

`--no-hooks`, `--no-pre-launch`, `--no-post-exit`, `--no-marker`, `--with-marker`, `--java` and `--wrapper` seed the instance's own `settings` block in `.shulker/instance.json`, the same way the other `link` commands do.

```sh
shulker link shulker
shulker link shulker https://github.com/shulker-sh/base-pack.git
shulker link shulker https://example.com/pack/shulker.json --as smp
```

| Flag | Description |
| --- | --- |
| `--as <nickname>` | Nickname for this instance, which names its folder and finds it with `-i` (default: from the pack's name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Repoint the modpack an instance already follows |
| `--no-hooks` | Install neither hook: don't sync before a launch, don't record how a run ended |
| `--no-pre-launch` | Don't sync this instance before each launch |
| `--no-post-exit` | Don't record how each run ended |
| `--no-marker` | Leave the marker mod out of this instance's builds |
| `--with-marker` | Include the marker mod in this instance's builds, over a manifest that leaves it out |
| `--java <path>` | Absolute path to the Java this machine launches the instance with (default: shulker's managed runtime) |
| `--wrapper <cmd>` | Command prefix for the launch command, such as `gamemoderun`; split on whitespace |

### `shulker link atlauncher`

Create an ATLauncher instance that syncs the client build before each launch.

shulker writes the instance itself: the Minecraft version, the loader if the project has one, and a pre-launch command that runs `shulker sync`. ATLauncher downloads the game, its libraries and Java the first time you press Play. For NeoForge and Forge, shulker runs the loader's installer once per loader version and copies what it builds into ATLauncher's `libraries` folder. A new instance gets the shulker image; an image you pick in ATLauncher is kept when you link again. ATLauncher only reads its instances when it starts, so restart it if it is open.

With no source, it links the project in the current directory. Pass a project directory, git URL, or manifest URL to link that instead. The instance is a project of its own: a `shulker.json` that follows the source as a modpack and builds where it stands, taking its platform, its features and its override layers from the pack at every sync, so `shulker add` in the game directory puts a mod on top of the pack and keeps it. shulker builds it before `link` returns, so it's ready to play, and keeps it up to date from the same source before each launch. The instance folder is named after the letters and digits in the instance name. Running `link` again keeps the settings you changed in ATLauncher, such as memory and Java arguments.

`--with` and `--without` are saved in the instance's own `shulker.local.json`. Change them later with `shulker feature on|off --into <instance folder>`, or run `link` again with new flags.

`link` never writes over a `shulker.json` that is already in the game directory: it adopts that project, clears the unlinked mark, rewrites the launcher's own files and builds it where it stands, so relinking an instance you unlinked picks it back up with everything you added on top of the pack. If the instance already follows a different modpack, or is an ATLauncher instance shulker didn't link, `link` fails rather than taking it over. Use `--name` to create a second instance, or `--force` to link over this one: `--force` repoints the modpack the instance follows and leaves everything you added on top of it where it is.

`--no-hooks`, `--no-pre-launch`, `--no-post-exit`, `--no-marker`, `--with-marker`, `--java` and `--wrapper` seed the instance's own `settings` block in `.shulker/instance.json`. The manifest's `client.hooks` are the defaults, a flag overrides one for this link, and from then on the file decides: no sync writes over it. The marker is not seeded: `settings.marker` is written only by `--no-marker` or `--with-marker`, and while it is absent every build reads the manifest's `marker`. Change your mind later by editing the file and running `shulker instances repair`.

`--wrapper` is written into the launcher's own wrapper setting, and only when you pass one: with no `--wrapper`, that setting stays yours. ATLauncher splits its wrapper on whitespace and ignores quotes, so a wrapper word with a space in it can't be written there.

```sh
shulker link atlauncher
shulker link atlauncher https://github.com/shulker-sh/base-pack.git
shulker link atlauncher https://example.com/pack/shulker.json --name "Friends SMP" --with shaders
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher data directory (default: ATLauncher's) |
| `--name <name>` | Instance name (default: the side's display name) |
| `--as <id>` | Id for this instance, which `-i` takes (default: derived from its name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Repoint the modpack an instance already follows, or link over one shulker didn't link |
| `--no-hooks` | Install neither hook: don't sync before a launch, don't record how a run ended |
| `--no-pre-launch` | Don't sync this instance before each launch |
| `--no-post-exit` | Don't record how each run ended |
| `--no-marker` | Leave the marker mod out of this instance's builds |
| `--with-marker` | Include the marker mod in this instance's builds, over a manifest that leaves it out |
| `--java <path>` | Absolute path to the Java this machine launches the instance with (default: shulker's managed runtime) |
| `--wrapper <cmd>` | Command prefix for the launch command, such as `gamemoderun`; split on whitespace |
| `--with <feature>` | Turn a feature on for this instance; repeat for more |
| `--without <feature>` | Turn a feature off for this instance; repeat for more |

### `shulker link gdlauncher`

Create a GDLauncher instance that syncs the client build before each launch.

shulker writes the instance's `instance.json` itself: the Minecraft version, the loader if the project has one, and a pre-launch hook that runs `shulker sync`. GDLauncher downloads the game, the loader and Java the first time you press Play, NeoForge and Forge included. Every `link` marks the instance for setup again, so the next Play re-checks the install and takes a little longer. It can only install loader versions on its own list, which trails new releases by a few days. When the locked loader version isn't on that list yet, the instance uses the newest one GDLauncher has and `link` warns you; run `link` again once GDLauncher adds it, or pass `--force` to use the locked version anyway. A new instance gets the shulker icon; linking again never changes the icon, so one you pick in GDLauncher, or the default, stays. GDLauncher only reads its instances when it starts, and while open it writes its own copy back over them when you change settings or play, so quit it before linking and open it afterwards. On macOS and Linux, `link` and `unlink` warn you when GDLauncher is open.

With no source, it links the project in the current directory. Pass a project directory, git URL, or manifest URL to link that instead. The instance is a project of its own: a `shulker.json` that follows the source as a modpack and builds where it stands, taking its platform, its features and its override layers from the pack at every sync, so `shulker add` in the game directory puts a mod on top of the pack and keeps it. shulker builds it before `link` returns, so it's ready to play, and keeps it up to date from the same source before each launch. The instance folder is named the way GDLauncher names it. Running `link` again keeps the settings you changed in GDLauncher, such as memory and Java arguments. If you moved GDLauncher's runtime path in its settings, shulker follows it.

Renaming the instance in GDLauncher moves its folder. It keeps syncing before each launch, but `shulker instances` reports it missing; run `link` again with the new `--name`, and `shulker unlink` the old one.

`--with` and `--without` are saved in the instance's own `shulker.local.json`. Change them later with `shulker feature on|off --into <game folder>`, or run `link` again with new flags.

`link` never writes over a `shulker.json` that is already in the game directory: it adopts that project, clears the unlinked mark, rewrites the launcher's own files and builds it where it stands, so relinking an instance you unlinked picks it back up with everything you added on top of the pack. If the instance already follows a different modpack, or is a GDLauncher instance shulker didn't link, `link` fails rather than taking it over. Use `--name` to create a second instance, or `--force` to link over this one: `--force` repoints the modpack the instance follows and leaves everything you added on top of it where it is.

`--no-hooks`, `--no-pre-launch`, `--no-post-exit`, `--no-marker`, `--with-marker`, `--java` and `--wrapper` seed the instance's own `settings` block in `.shulker/instance.json`. The manifest's `client.hooks` are the defaults, a flag overrides one for this link, and from then on the file decides: no sync writes over it. The marker is not seeded: `settings.marker` is written only by `--no-marker` or `--with-marker`, and while it is absent every build reads the manifest's `marker`. Change your mind later by editing the file and running `shulker instances repair`.

`--wrapper` is written into the launcher's own wrapper setting, and only when you pass one: with no `--wrapper`, that setting stays yours.

```sh
shulker link gdlauncher
shulker link gdlauncher https://github.com/shulker-sh/base-pack.git
shulker link gdlauncher https://example.com/pack/shulker.json --name "Friends SMP" --with shaders
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher runtime directory (default: GDLauncher's) |
| `--name <name>` | Instance name (default: the side's display name) |
| `--as <id>` | Id for this instance, which `-i` takes (default: derived from its name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Repoint the modpack an instance already follows, link over one shulker didn't link, and use the locked loader version even if GDLauncher can't install it yet |
| `--no-hooks` | Install neither hook: don't sync before a launch, don't record how a run ended |
| `--no-pre-launch` | Don't sync this instance before each launch |
| `--no-post-exit` | Don't record how each run ended |
| `--no-marker` | Leave the marker mod out of this instance's builds |
| `--with-marker` | Include the marker mod in this instance's builds, over a manifest that leaves it out |
| `--java <path>` | Absolute path to the Java this machine launches the instance with (default: shulker's managed runtime) |
| `--wrapper <cmd>` | Command prefix for the launch command, such as `gamemoderun`; split on whitespace |
| `--with <feature>` | Turn a feature on for this instance; repeat for more |
| `--without <feature>` | Turn a feature off for this instance; repeat for more |

### `shulker link mojang`

Install the project's loader, if it has one, into the official launcher and add a profile that points at the client build. Alias: `vanilla`.

With no source, it links the project in the current directory. Pass a project directory, git URL, or manifest URL to link that instead. Either way the game directory is `shulker/<slug>` inside the launcher directory, and what lands there is a project of its own: a `shulker.json` that follows the source as a modpack and builds where it stands. It takes its platform, its features and its override layers from the pack at every sync, so `shulker add` in the game directory puts a mod on top of the pack and keeps it. shulker builds it before `link` returns, so it's ready to play. The official launcher runs no commands of its own, so shulker points the profile's Java at a small shim of its own that syncs the instance before each launch and then starts the game. That shim lives in the instance's own `.shulker` folder: a shell script on macOS and Linux, and on Windows a small executable shulker generates there, beside a file holding the two paths it needs.

`link` never writes over a `shulker.json` that is already in the game directory: it adopts that project, clears the unlinked mark, rewrites the launcher's own files and builds it where it stands, so relinking an instance you unlinked picks it back up with everything you added on top of the pack. If the profile already follows a different modpack, `link` fails rather than repointing it. Use `--name` to create a second profile, or `--force` to repoint this one: `--force` repoints the modpack the instance follows and leaves everything you added on top of it where it is.

`--no-hooks`, `--no-pre-launch`, `--no-post-exit`, `--no-marker`, `--with-marker`, `--java` and `--wrapper` seed the instance's own `settings` block in `.shulker/instance.json`. The manifest's `client.hooks` are the defaults, a flag overrides one for this link, and from then on the file decides: no sync writes over it. The marker is not seeded: `settings.marker` is written only by `--no-marker` or `--with-marker`, and while it is absent every build reads the manifest's `marker`. Change your mind later by editing the file and running `shulker instances repair`.

`--wrapper` prefixes the Java command the shim runs, since the official launcher has no wrapper setting of its own.

```sh
shulker link mojang
shulker link mojang https://github.com/shulker-sh/base-pack.git
shulker link mojang https://example.com/pack/shulker.json --name "Friends SMP"
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher directory (default: the official launcher's `.minecraft` folder) |
| `--name <name>` | Profile name (default: the side's display name) |
| `--as <id>` | Id for this instance, which `-i` takes (default: derived from its name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Repoint the modpack a profile already follows |
| `--no-hooks` | Install neither hook: don't sync before a launch, don't record how a run ended |
| `--no-pre-launch` | Don't sync this instance before each launch |
| `--no-post-exit` | Don't record how each run ended |
| `--no-marker` | Leave the marker mod out of this instance's builds |
| `--with-marker` | Include the marker mod in this instance's builds, over a manifest that leaves it out |
| `--java <path>` | Absolute path to the Java this machine launches the instance with (default: shulker's managed runtime) |
| `--wrapper <cmd>` | Command prefix for the launch command, such as `gamemoderun`; split on whitespace |

### `shulker link prism`

Create a Prism Launcher instance that syncs the client build before each launch.

With no source, it links the project in the current directory. Pass a project directory, git URL, or manifest URL to link that instead. The instance is a project of its own: a `shulker.json` that follows the source as a modpack and builds where it stands, taking its platform, its features and its override layers from the pack at every sync, so `shulker add` in the game directory puts a mod on top of the pack and keeps it. shulker builds it before `link` returns, so it's ready to play, and keeps it up to date from the same source before each launch. Nothing is created in the directory you ran it from.

`--with` and `--without` are saved in the instance's own `shulker.local.json`. Change them later with `shulker feature on|off --into <game dir>`, or run `link` again with new flags.

`link` never writes over a `shulker.json` that is already in the game directory: it adopts that project, clears the unlinked mark, rewrites the launcher's own files and builds it where it stands, so relinking an instance you unlinked picks it back up with everything you added on top of the pack. If the instance already follows a different modpack, `link` fails rather than repointing it. Use `--name` to create a second instance, or `--force` to repoint this one: `--force` repoints the modpack the instance follows and leaves everything you added on top of it where it is. On the next sync, files the old pack put there are removed, unless you changed them in-game.

`--no-hooks`, `--no-pre-launch`, `--no-post-exit`, `--no-marker`, `--with-marker`, `--java` and `--wrapper` seed the instance's own `settings` block in `.shulker/instance.json`. The manifest's `client.hooks` are the defaults, a flag overrides one for this link, and from then on the file decides: no sync writes over it. The marker is not seeded: `settings.marker` is written only by `--no-marker` or `--with-marker`, and while it is absent every build reads the manifest's `marker`. Change your mind later by editing the file and running `shulker instances repair`.

`--wrapper` is written into the launcher's own wrapper setting, and only when you pass one: with no `--wrapper`, that setting stays yours.

```sh
shulker link prism
shulker link prism https://github.com/shulker-sh/base-pack.git
shulker link prism https://example.com/pack/shulker.json --name "Friends SMP" --with shaders
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | Launcher data directory (default: Prism Launcher's) |
| `--name <name>` | Instance name (default: the side's display name) |
| `--as <id>` | Id for this instance, which `-i` takes (default: derived from its name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Repoint the modpack an instance already follows |
| `--no-hooks` | Install neither hook: don't sync before a launch, don't record how a run ended |
| `--no-pre-launch` | Don't sync this instance before each launch |
| `--no-post-exit` | Don't record how each run ended |
| `--no-marker` | Leave the marker mod out of this instance's builds |
| `--with-marker` | Include the marker mod in this instance's builds, over a manifest that leaves it out |
| `--java <path>` | Absolute path to the Java this machine launches the instance with (default: shulker's managed runtime) |
| `--wrapper <cmd>` | Command prefix for the launch command, such as `gamemoderun`; split on whitespace |
| `--with <feature>` | Turn a feature on for this instance; repeat for more |
| `--without <feature>` | Turn a feature off for this instance; repeat for more |

### `shulker link multimc`

Create a MultiMC instance that syncs the client build before each launch. It is [`shulker link prism`](#shulker-link-prism) for MultiMC's own `instance.cfg` dialect, with the same source argument, flags and behaviour, and one difference: MultiMC is portable and has no fixed data folder, so `--launcher-dir` is required and names the folder that holds `multimc.cfg` (`launcher-dir-required` without it). The instance is registered under the launcher name `multimc`, which is what `--launcher multimc` and `shulker unlink multimc` match.

`--no-hooks`, `--no-pre-launch`, `--no-post-exit`, `--no-marker`, `--with-marker`, `--java` and `--wrapper` seed the instance's own `settings` block in `.shulker/instance.json`. The manifest's `client.hooks` are the defaults, a flag overrides one for this link, and from then on the file decides: no sync writes over it. The marker is not seeded: `settings.marker` is written only by `--no-marker` or `--with-marker`, and while it is absent every build reads the manifest's `marker`. Change your mind later by editing the file and running `shulker instances repair`.

`--wrapper` is written into the launcher's own wrapper setting, and only when you pass one: with no `--wrapper`, that setting stays yours.

```sh
shulker link multimc --launcher-dir ~/MultiMC
shulker link multimc https://github.com/shulker-sh/base-pack.git --launcher-dir ~/MultiMC
```

| Flag | Description |
| --- | --- |
| `--launcher-dir <path>` | The MultiMC folder, the one that holds `multimc.cfg` (required) |
| `--name <name>` | Instance name (default: the side's display name) |
| `--as <id>` | Id for this instance, which `-i` takes (default: derived from its name) |
| `--ref <ref>` | Branch, tag, or commit to follow from a git source (default: the remote HEAD) |
| `--force` | Repoint the modpack an instance already follows |
| `--no-hooks` | Install neither hook: don't sync before a launch, don't record how a run ended |
| `--no-pre-launch` | Don't sync this instance before each launch |
| `--no-post-exit` | Don't record how each run ended |
| `--no-marker` | Leave the marker mod out of this instance's builds |
| `--with-marker` | Include the marker mod in this instance's builds, over a manifest that leaves it out |
| `--java <path>` | Absolute path to the Java this machine launches the instance with (default: shulker's managed runtime) |
| `--wrapper <cmd>` | Command prefix for the launch command, such as `gamemoderun`; split on whitespace |
| `--with <feature>` | Turn a feature on for this instance; repeat for more |
| `--without <feature>` | Turn a feature off for this instance; repeat for more |

### `shulker sync`

Download and build one side of a project straight into a directory, without setting up a project there. The source can be a project directory, a git URL, or a manifest URL. Worlds, logs, screenshots and crash reports stay in the directory you sync into, and nothing is written into the source project; only the project's own build directories link them to its `data/<side>/`.

```sh
shulker sync https://github.com/shulker-sh/base-pack.git --side server --into /srv/minecraft
shulker sync ../my-pack --side client --into ~/instances/my-pack
shulker sync -i friends-smp
shulker sync --into ~/instances/my-pack
shulker sync --all --side server
shulker sync
```

With `--into` and no source, shulker reads what the directory syncs from out of its own `.shulker/instance.json`, so a synced directory keeps working even if the registry is gone.

If a git or manifest URL can't be reached because the network is down, `sync` warns and builds from the copy used by the last sync from that source that succeeded, so an instance still launches offline. The warning names the commit and says how old that copy is. A server that answers with an error, a missing ref, or a failed login still fails the sync, and so does a source that has never synced successfully here. `--offline` skips the network entirely, which is quicker than waiting for timeouts on a network that drops traffic. For the server side, an installed Java runtime is kept when its update check can't reach the network.

A directory `sync --into` fills gets a `.shulker/instance.json` recording what it syncs from. An index of the instances shulker keeps in sync lives in `registry.json` beside shulker's `config.json` (a `registry` path in `config.json`, relative to that file, moves it), and [`link`](#shulker-link-prism) is what adds to it. A `sync --into` directory is a detached build, not an instance: it takes no row, so it never shows up in [`shulker instances`](#shulker-instances) or under `-i`. What records it is the source project's `shulker.local.json`, which is where [`diff`](#shulker-diff) and [`pull`](#shulker-pull) find it.

To update an instance, name it instead of a source. `-i` takes an instance's id, its name, or its directory, and syncs it from the modpack its `shulker.json` follows. Ids are unique, so `-i <id>` always picks exactly one; a name several instances share needs `--launcher` or `--side` to narrow it, or `--all` to sync them all. `--all` alone syncs every instance. It keeps going when one fails, and exits with an error at the end. With no source and neither flag, `sync` run inside a project syncs every instance synced from that project and every directory it has synced into, narrowed by `--launcher` or `--side`. Outside a project it asks which one to sync when run in a terminal, and fails with the list otherwise.

A project whose side builds into its own directory is an instance, and `sync` run inside it (or naming it with `-i`) updates the instance itself first: modpacks that follow their source are fetched again (every modpack except one set to `"autoUpdate": false`), the lock is resolved against them without moving your own mods, and the side is built in place. Nothing is written, and no history entry is taken, when the lock comes out unchanged. Every instance synced from it is synced after, since those build from its lock. A modpack update your own mods can't satisfy stops the sync with the reason, leaving the lock and the directory as they were; a launcher's pre-launch hook instead builds what the lock already has and starts the game.

| Flag | Description |
| --- | --- |
| `--into <path>` | Output directory (default: the side's build directory) |
| `--all` | Sync every instance `-i` matches, or every instance when there's no `-i` |
| `--launcher <launcher>` | Only instances linked in this launcher: `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--side <side>` | Side to build from a source (default: the only declared side); with `-i`, `--all`, or the picker, only `client` or `server` instances |
| `--offline` | Don't use the network; build from the last successful sync and cached files |
| `--force` | Overwrite files edited in the output directory |
| `--assume-client` | Build a client even when the source declares none, from the mods and overrides both sides share; recorded in the directory so later syncs keep building it |
| `--ref <ref>` | Branch, tag, or commit to sync from a git source (default: the remote HEAD) |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |

### `shulker instances`

List the instances shulker keeps in sync, grouped by launcher. Each row leads with the instance's id, which is what `-i` takes, and shows its side, when it was last synced, its directory, and the name and source it syncs from. A directory that is gone or can't be read is flagged, and so is one missing its `.shulker/instance.json`. When the last sync failed, the row says so and names why, beside the time of the sync that built what is on disk. When the last launch never got as far as running the game, a line under the row says so and names the reason.

```sh
shulker instances
```

```
Prism Launcher
  friends-smp (client), synced 2026-09-11 14:02
    ~/Library/Application Support/PrismLauncher/instances/shulker-friends-smp/minecraft
    Friends SMP, from https://github.com/shulker-sh/base-pack.git, side client

Minecraft Launcher
  my-pack (client), synced 2026-09-10 21:40
    ~/Library/Application Support/minecraft/shulker/my-pack
    My Pack, from https://github.com/shulker-sh/base-pack.git, ref v3, side client
```

### `shulker instances repair`

Put the registry back in step with what is on disk. It works even when `registry.json` can't be read, rewriting it from what it finds: it scans each launcher's own instances directory and shulker's own instances root, registers any folder shulker syncs that isn't in the index and wasn't unlinked, and writes a `.shulker/instance.json` for any instance missing one. A registered directory that is gone is reported rather than dropped, since an unmounted disk looks exactly like a deleted instance; [`shulker unlink`](#shulker-unlink) is what forgets one. `shulker self update` runs it after a successful update.

A folder counts as one shulker syncs when it holds a `shulker.json` that builds where it stands — the project a [`link`](#shulker-link-prism) leaves in a game directory — or a `.shulker/` shulker wrote before, so an instance that lost its `.shulker/` is found by its manifest alone. Such an instance follows the one modpack that manifest requires, comes back under the id it was linked as, which the manifest's `name` records, takes its name from the launcher's own file, and gets an instance file holding only its settings, since the manifest holds the rest. A directory that is no project of its own reads what it syncs from in its instance file, or in what its last build recorded where that file is the part that went missing. An instance marked unlinked stays unregistered either way.

Repair writes rows and instance files, never a launcher's pre-launch command: a directory it finds again is listed and syncs with `-i`, and a [`link`](#shulker-link-prism) is what makes the launcher refresh it before each launch.

A row written again keeps the time of the last sync that worked, which the directory's own `.shulker/instance.json` records, even when the sync after it failed: that time is when the directory was last built correctly. The message from that failure isn't kept, since it lives only on the registry row, so [`shulker instances`](#shulker-instances) reports no failure for a repaired row until the next sync.

```sh
shulker instances repair
shulker instances repair --launcher prism
shulker instances repair --launcher prism --launcher-dir ~/other-prism
```

| Flag | Description |
| --- | --- |
| `--launcher <launcher>` | Only scan this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--launcher-dir <path>` | Scan this directory instead of the launcher's own, or instead of the instances root for `shulker`; needs `--launcher` |

### `shulker instance`

`instance get`, `instance set`, `instance unset` and `instance edit` read and change the settings of one instance: the one the current directory is, or the one `-i` names from anywhere. That is its `.shulker/instance.json`, and every directory shulker syncs into has one, so an instance in another launcher has settings here too. The plural [`shulker instances`](#shulker-instances) is the list of them.

A path is relative to the file's `settings` block and dotted the way `shulker set` dots the manifest: `memory`, `hooks.preLaunch`. Five settings are launch settings with a default in `config.json` under `play.`, which [`shulker config set`](#shulker-config-set) sets for every instance at once: an instance that sets one wins, and one that doesn't inherits the default. They apply when shulker launches the instance itself, with [`shulker play`](#shulker-play). `play.java` and `play.wrapper` don't reach another launcher, but an instance's own `java` and `wrapper` still point that launcher at a Java and a wrapper, as before.

| Setting | Default | Description |
| --- | --- | --- |
| `memory` | `play.memory` | Heap size, as `-Xms` and `-Xmx`, like `6G`. Without either, the JVM picks its own |
| `jvmArgs` | `play.jvmArgs` | Extra JVM arguments, after the version's own and the memory, so one of them wins over both. An instance's list replaces the default rather than adding to it |
| `java` | `play.java` | Absolute path to a java binary, or for `play` a Java home. Without either, shulker's managed runtime |
| `window` | `play.window` | Window size, like `1280x720`. `play --window` wins over both for one run |
| `wrapper` | `play.wrapper` | A command the launch runs through, like `["gamemoderun"]` |
| `account` | | The account `play` launches this instance as, over the default account |
| `marker` | | Whether to include the marker mod, over the manifest's own `marker` |
| `hooks.preLaunch`, `hooks.postExit` | | Whether another launcher syncs before each launch, and records each run |
| `launchHistory` | | How many launch records to keep |

### `shulker instance get`

Print a setting as it is in effect, then where it came from: set in this instance, with the default it would return to; its `play.` default; or the setting's own default. A setting neither the instance nor `config.json` sets fails with `path-not-set`. With no path, print every setting the instance has, with the `play.` defaults it inherits filled in.

```sh
| `savesGroup` | | The save group whose worlds this instance shares, `default` unless set; `none` keeps them in the instance. See [`shulker saves`](#shulker-saves) |
shulker instance get memory
shulker -i smp instance get window
shulker instance get
```

With `--json`, the data is `{ "path", "value", "from", "default" }`, where `from` is `instance`, `config` or `default`, and `default` is what `instance unset` returns the setting to, absent when there is nothing to return to. With no path it is the settings object.

### `shulker instance set`

Set a setting in this instance, over its default. The value is checked as it is set, against the instance file's schema: a list needs `--literal`, and `java` must be an absolute path. `account` takes an account's name or id, matched the way [`shulker accounts use`](#shulker-accounts-use) matches one, and records its id, so the pin still holds after the player renames themselves; an account shulker can't see fails with `account-not-found`.

```sh
shulker instance set memory 8G
shulker instance set window 1920x1080
shulker instance set jvmArgs --literal '["-XX:+UseZGC"]'
shulker instance set account Notch
shulker -i smp instance set hooks.preLaunch false
```

| Flag | Description |
| --- | --- |
| `--literal` | Parse the value as JSON, for a list |

With `--json`, `instance set` and `instance unset` return `{ "path", "from", "to" }` like `set`.

### `shulker instance unset`

Remove a setting from this instance, so it returns to its default. Removing one that isn't set succeeds and says so.

```sh
shulker instance unset memory
```

### `shulker instance edit`

Open the instance's `instance.json` in `$VISUAL` or `$EDITOR`, or in `vi` (`notepad` on Windows) when neither is set, and check the file once the editor exits. It opens a file that no longer matches its schema too, since that is the file most in need of an editor; a save that still doesn't fails with `instance-invalid` and leaves what you saved in place for the next edit. It needs a terminal, so under `--no-input` or off one it is a usage error, and an editor that exits with an error fails with `editor-failed`.

With `--json`, the data is `{ "file", "changed" }`.

### `shulker unlink`

Stop syncing a linked instance and remove it from the list. Its files, worlds, and feature choices stay. For a Prism Launcher or MultiMC instance, `unlink` removes the pre-launch sync but keeps the instance. It leaves a pre-launch command alone if you replaced shulker's with your own. For the official launcher, it removes the profile but keeps the instance directory and the installed loader. The directory's `.shulker/instance.json` is marked unlinked, so [`shulker instances repair`](#shulker-instances-repair) doesn't register it again; linking or syncing into it clears the mark. `unlink` also drops the directory from its source project's `shulker.local.json`, so a bare `shulker sync` there no longer builds it. A `sync --into` directory is a detached build with no row on the list, so `unlink` doesn't take one.

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

### `shulker saves`

Every instance [`shulker link shulker`](#shulker-link-shulker) makes shares its worlds through a save group: its `saves/` folder is a link (a directory junction on Windows) to the group's folder under the saves root, so every instance in a group lists the same worlds. Each joins `default`. Set another group with `shulker instance set savesGroup <name>`, or `none` to keep the worlds in the instance; the next sync relinks it and names the worlds the game now lists. Nothing is copied or merged: leaving a group leaves its worlds there. Joining one replaces an empty `saves/`, and one holding worlds is moved in as the group when the group has none; when both hold worlds, the sync warns and leaves `saves/` alone until you merge them by hand. Instances in other launchers keep their own worlds.

With no target, `saves` lists the groups with how many worlds each holds, its size, and when it was last backed up. With `-i`, `-C` or `--group`, it lists that target's worlds and its backups, newest first. A shulker instance in a group shows the group; any other directory shows its own `saves/` and the backups in its `.shulker/backups/`. A group's backups live in `backups/<group>` in shulker's data directory.

```sh
shulker saves
shulker saves -i smp
shulker saves --group default
```

| Flag | Description |
| --- | --- |
| `--group <group>` | Show this save group rather than an instance |

With `--json`, the list is `[{ "name", "dir", "worlds", "size", "lastBackup" }]`, and a target is `{ "group", "dir", "worldsDir", "worlds", "backups" }` with each backup `{ "n", "id", "path", "taken", "reason", "size" }`.

### `shulker saves prune`

Delete all but the newest `--keep` backups of a save group or instance, whichever backup took them. `--keep` is required, so a prune always says how many survive. The target is the one `shulker saves` would show: `--group`, `-i`, `-C`, or the current directory.

```sh
shulker saves prune --group default --keep 3
shulker -i smp saves prune --keep 0
```

| Flag | Description |
| --- | --- |
| `--keep <n>` | How many of the newest backups to keep (required) |
| `--group <group>` | Prune this save group's backups rather than an instance's |

With `--json`, the data is `{ "group", "dir", "worldsDir", "pruned", "kept" }`.

| Flag | Description |
| --- | --- |
| `--deadline <duration>` | Stop the update after this long and say so, aborting the launch (default: no deadline) |

### `shulker hook post-exit`

What a launcher's own post-exit slot runs, recording how the run ended in the instance's `.shulker/launches.json`: when it started and finished, whether the game left a crash report, and where that report and the log are. `settings.launchHistory` in `.shulker/instance.json` is how many runs are kept — 5 by default, `-1` every one, and `0` none at all, which records nothing.

A run's `outcome` is `ok`, `crashed`, or `not-started` for one the game never began, which also carries the reason in `error` and has the same `startedAt` and `endedAt`. A run [`play`](#shulker-play) started also has `exitCode`, the status the game left, which no launcher passes to its post-exit slot; and while it is still going, `pid`, the game's own process, which the record drops once it is closed.

### `shulker hook wrap -- <java arguments>`

What the Mojang launcher's `javaDir` shim runs in place of Java, with the instance in `-C` and the game's own arguments after `--`. When those arguments carry `--gameDir` it does what the pre-launch and post-exit hooks do around the game: syncs the instance first (a failure is a warning, and the game still starts), runs Java with the arguments untouched, prefixed by `settings.wrapper` when that is set, then records the run. A `settings.wrapper` that can't be run at all is a warning and the game starts with Java on its own. Without `--gameDir` it is the launcher's version check, which only runs Java. Java is `settings.java` when set, else `resolved.java`, shulker's managed runtime. The game's exit status is passed back as its own (`game-exit`).

Where no game started at all, `hook wrap` exits `launch-not-started`: the instance file couldn't be read, no Java is recorded, or the recorded Java wouldn't start. That exit is what makes the launcher raise an error, which is all a player sees when no window appears, and the last two also leave a `not-started` launch record, so `instances` and the launch history say the launch never happened. Shulker's own failures around a launch that is going ahead never turn into one: a failed sync and a wrapper that gave way both exit 0, because the game is starting either way. The arguments carry the session access token and appear in no output or record.

### `shulker watch`

The watcher a detached [`play`](#shulker-play) leaves behind, and not something to run by hand. It reads the launch from its stdin — never from its arguments, since the game's own arguments carry the session access token and a process list is public — starts the game, writes one line back with the game's process id, or with why it couldn't start it, then waits for the game to exit and records the run. It writes nothing else anywhere but the launch history. On Windows it runs with no console at all, and starts the game with none either, so neither opens a window.

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

### `shulker self uninstall`

Take shulker out of every launcher it hooked, then remove the binary. It walks the registry: each instance gets its pre-launch and post-exit scripts, its slots and its generated shim removed, a command shulker adopted goes back where it came from, and an official launcher profile gets its own Java back. Instances whose folders have moved away are still unhooked, from what the registry records about them.

Nothing else goes. Every instance folder stays, with its worlds and its builds, and so does the registry, so reinstalling shulker and running [`shulker instances repair`](#shulker-instances-repair) puts every hook back. Nothing prompts: running the command is the intent.

`--purge` also forgets the registry. Then nothing is left of shulker on the machine, and a repair after reinstalling finds only the instances the launchers themselves hold, so `--purge` names any row no launcher holds before it goes.

An instance shulker can't unhook — an unreadable launcher file, say — is a warning, and the rest of the uninstall carries on. On Windows the running binary can't be deleted, so it is renamed to `shulker.exe.old` and the last line tells you to delete it.

```sh
shulker self uninstall
shulker self uninstall --purge
```

| Flag | Description |
| --- | --- |
| `--purge` | Also forget the registry, the index `instances repair` rebuilds from |

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

`candidates` lists values you could pass instead, like the sides when a command is given something that is not one. `items` lists what the error is about, like the files in conflict. Both are left out when empty.

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
| `account-exists` | An account already answers to that name, or already plays under that UUID. A name is shared with `--force`; a UUID never is |
| `account-name-invalid` | An offline name outside the pattern a Minecraft username matches; `--allow-invalid-name` takes it |
| `account-not-found` | No account matches the selector, or the account an instance is pinned to has since been removed. `candidates`: every account, each as its qualifier and its id, `pass`: their ids |
| `account-not-playable` | The account owns no Java profile, so it can't launch anything |
| `account-sign-in-expired` | The Microsoft refresh token is gone or revoked, so shulker can't get a session for the account; `shulker accounts login` signs it in again |
| `accounts-invalid` | shulker's own `accounts.json` doesn't parse or doesn't match its schema |
| `already-ignored` | The pair already has an ignore in `shulker.json`; pass `--force` to replace it |
| `ambiguous-account` | Several accounts match the selector and shulker can't ask, because it isn't running on a terminal. `candidates`: the matches, each as its qualifier and its id, `pass`: their ids |
| `ambiguous-instance` | Several instances match the name given. `candidates`: the matches, `pass`: their ids, which are unique |
| `ambiguous-into` | The side has edits in several synced directories; pass `--into`. `candidates`: the directories |
| `ambiguous-side` | The manifest declares both sides and the command works on one; `sync` and `pull` take `--side`, `diff --into` names it. `candidates`: the sides |
| `build-conflict` | Files changed both in the build directory and in the source; run `diff`, or pass `--force` to overwrite. `items`: the files |
| `build-reserved` | A side that builds in place has overrides that would write `shulker.json`, `shulker.lock`, `shulker.local.json`, `.shulker/` or a data directory. `items`: the files |
| `cache-root-unreadable` | A registered instance's `shulker.lock` is there but can't be read, so `cache prune` stops rather than remove files that instance may need; `cache info` still reports and names the instance |
| `config-invalid` | shulker's `config.json` isn't valid JSON; the message names the line and column. Only commands that need its registry location fail; the rest warn and go on without it |
| `curseforge-key-rejected` | CurseForge rejected the API key: your own, or shulker's built-in one when shulker.sh has no working replacement |
| `curseforge-not-found` | `export curseforge` found nothing on CurseForge for these mods, resource packs or shaders; pass `--bundle`. `items`: what is missing |
| `deps-held` | A mod being added needs another version of a dependency the lock holds; `--with-deps` moves them. `items`: each held version and what needs it |
| `registry-has-instances` | `config set` or `config unset` would move the registry away from instances the new one doesn't have; `--force` changes it anyway. `items`: the directories left behind |
| `registry-invalid` | shulker's `registry.json`, the list of linked instances and synced directories, isn't valid JSON; the message names the line and column |
| `editor-failed` | The editor `instance edit` ran couldn't be started or exited with an error; set `$EDITOR` to the one you use |
| `error` | Anything unexpected, like a file that can't be read or written. The message has the details |
| `eula-required` | The server needs the Minecraft EULA accepted |
| `feature-not-found` | No mod or feature declaration uses the feature. `candidates`: the features in use |
| `file-not-found` | A file named to `pull` isn't in the build directory |
| `game-exit` | The game `hook wrap` ran exited with an error; the exit status is the game's own |
| `git-missing` | A git source needs `git` on PATH |
| `history-empty` | The instance has no history entries yet; one is taken before an in-place build changes anything |
| `history-invalid` | A history entry's own record is unreadable; `history prune` removes it |
| `history-missing` | There is no history entry with that number; the message says how many are kept |
| `installer-failed` | NeoForge's or Forge's own installer failed while setting up a server dir or a launcher; the message shows its last output and names the log in shulker's cache that holds all of it |
| `instance-exists` | An instance already follows a different modpack, or is an ATLauncher or GDLauncher instance shulker didn't link; pass `--name` for a second one, or `--force` |
| `instance-missing` | A linked instance's directory is gone |
| `group-not-found` | `--group` names a save group that isn't under the saves root |
| `source-unknown` | `sync --into` found no record in the directory of what it was synced from; name the source |
| `instance-not-found` | No instance matches, or the directory `shulker instance` acts on holds no `.shulker/instance.json`. `candidates`: the instances shulker knows, `pass`: their ids |
| `instance-id-taken` | Another instance already has the `--as` id; the message names its directory |
| `instance-invalid` | An instance's `.shulker/instance.json` doesn't parse, doesn't match its schema, or names a `$schema` this shulker doesn't know; `shulker instances repair` writes it again |
| `interrupted` | Ctrl-C or SIGTERM stopped the command. Files are left whole: each one is written in full or not at all. A second Ctrl-C quits at once |
| `into-missing` | The `--into` directory does not exist |
| `into-required` | Syncing from a remote source needs `--into` |
| `java-not-found` | No working Java at the configured path or on PATH |
| `java-range` | `java` in `shulker.json` is neither a path nor a version range |
| `java-version` | The Java found is outside the range in `shulker.json` |
| `jvm-flags` | Unknown `jvmFlags` preset |
| `key-not-found` | A `--key` isn't in the file. `candidates`: its keys |
| `launch-not-started` | Shulker never got as far as running the game: for `hook wrap`, the instance file couldn't be read, no Java is recorded, or the recorded Java wouldn't start; for `play`, the Java it assembled wouldn't start, or the watcher it hands a detached launch to couldn't be started or stopped before it answered. Under a launcher the exit is what makes it show an error, since no window appears |
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
| `no-accounts` | shulker can see no account at all, so there is nothing to play with |
| `no-compatible-version` | The mod has no version for this Minecraft and loader. `candidates`: other release channels that have one |
| `no-instances` | Nothing is linked yet |
| `no-problem` | The locked mods have no dependency problem for the pair; pass `--rule` and `--declared` from the failed command. `candidates`: the current problems, where there are any |
| `no-side` | `shulker.json` declares no side of the kind the command needs. A local command (`build`, `diff`, `serve`) says to add the block; a command that can take a remote source (`sync`, `export *`) says to pass `--assume-client` |
| `not-built` | The side has no build directory yet; run `shulker build` |
| `not-direct` | The mod is only a dependency. `items`: the mods that require it |
| `not-drifted` | A file named to `pull` has no changes. `candidates`: the changed files |
| `not-ignored` | The pair has no ignore in `shulker.json`. `candidates`: the pairs that do |
| `not-in-place` | The project has no side that builds in place, so it keeps no history |
| `not-installed` | A file isn't in the cache; run `shulker install` |
| `not-pinned` | The mod has no pin |
| `not-shulker` | The instance belongs to another launcher, which starts it itself |
| `not-synced` | The directory has no record of the source it was synced from |
| `modpack-changed` | A modpack no longer matches the lock; run `shulker update` |
| `modpack-conflict` | Two modpacks list the same mod with different settings |
| `modpack-exists` | The modpack is already in `shulker.json` |
| `modpack-fetch` | A modpack couldn't be fetched |
| `modpack-lock-missing` | A modpack is set `locked: true` but its source has no `shulker.lock`; run `shulker lock` there, or set locked false |
| `modpack-manifest` | A modpack source has no `shulker.json` |
| `modpack-mismatch` | A modpack wants a different Minecraft version or loader |
| `modpack-not-found` | The modpack isn't in `shulker.json`. `candidates`: the modpacks |
| `modpack-platform` | Locked modpacks disagree about Minecraft or the loader, and `shulker.json` sets neither; set `minecraft`/`loader`, or unlock one |
| `modpack-provided` | The mod comes from a modpack, so it can't be removed on its own |
| `modpack-ref` | A modpack's `ref` doesn't apply to its source, or wasn't found |
| `modpack-unlocked` | A modpack has no commit in the lock; run `shulker update` |
| `ownership-unproven` | Shulker can see no account that owns Minecraft: Java Edition, so it won't create an offline account — or delete one, since the same gate would block creating it again; `--force` deletes it anyway |
| `path-invalid` | `shulker.json`, `config.json` or an instance's settings have no such field, or the path goes inside a single value or a list. `candidates`: the fields allowed there |
| `path-not-set` | `get`, `config get` or `instance get` names a field that isn't set |
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
| `runtime-unavailable` | Mojang publishes no Java runtime for this platform. The `Fix:` row depends on the side: a server sets `java` in `shulker.json`, a client instance passes `--java <path>` to `shulker link` |
| `schema-newer` | A file shulker manages was written by a newer shulker, and this one can't read it; `shulker self update` catches up |
| `self-uninstall` | The shulker binary couldn't be removed |
| `self-update-check` | Checking for a release failed, or none is published |
| `self-update-checksum` | The download doesn't match its checksum |
| `self-update-download` | The download failed |
| `self-update-install` | The running binary couldn't be replaced |
| `self-update-provenance` | `--require-attestation` is set and the build provenance couldn't be verified |
| `sign-in-failed` | The Microsoft sign-in didn't finish: it was declined, the code ran out before it was used, or Microsoft or Xbox Live refused it — including an account with no Xbox profile, which can't reach Minecraft at all |
| `server-exit` | The server exited with an error. `items`: its `logs/latest.log` and, when the server wrote one during the run, its crash report; `data` carries them as `log` and `crashReport` |
| `source-fetch` | The sync source couldn't be fetched |
| `source-lock` | The sync source has no `shulker.lock` |
| `source-offline` | Offline, and the source has never synced here, so there's no copy to use |
| `source-ref` | `--ref` doesn't apply to the source, or wasn't found |
| `store-incomplete` | The game store can't supply what a launch needs: a file with no source that isn't on disk, a download whose checksum doesn't match, or a version JSON that doesn't hold together |
| `sync-failed` | Some entries failed to sync; `data` has each entry's result |
| `topic-not-found` | `docs` found no page, heading or line matching the words. `candidates`: the pages |
| `type-ambiguous` | A CurseForge slug matches projects of several types; pass `--type` to choose. `candidates`: the types it matched |
| `type-mismatch` | `--type` disagrees with what the provider says the project is. `candidates`: the provider's own type |
| `unlink-failed` | Some entries couldn't be unlinked; `data` has each entry's result |
| `unset-variable` | An override uses a variable that isn't set |
| `unsupported-loader` | shulker doesn't support the loader yet |
| `update-paused` | The pre-launch hook stopped a GDLauncher update at four minutes so it could explain itself; the launch is aborted, and launching again resumes it. Shown in GDLauncher's own dialog, so it prints without shulker's usual error decoration |
| `usage` | An unknown command or flag, wrong arguments, or a flag value that isn't allowed. `items`: the missing or unexpected arguments, when that's the problem. Exits 2 |
| `validation-failed` | The locked mods have dependency problems; each prints the `shulker ignore` command that would accept it. `items`: the problems |
| `version-not-found` | The provider has no version with the id given to `add --pin` or `pin`; the message links the mod's versions page |
| `version-required` | `export mrpack` and `export curseforge` need a version |
