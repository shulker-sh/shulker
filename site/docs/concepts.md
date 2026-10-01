---
description: How manifests, locks, sides, dependencies, overrides, features, modpacks, instances, history, saves, and providers fit together.
outline: [2, 3]
---

# Concepts

## Manifest

`shulker.json` declares the Minecraft version, loader, sides, mods, and modpacks. [`shulker create`](/docs/cli#shulker-create) writes one from flags, taking the defaults for anything not given, and [`shulker init`](/docs/cli#shulker-init) asks about each choice on the terminal instead. Every field is listed in the [Manifest Reference](/docs/manifest).

## Lock

`shulker.lock` records the exact resolved versions, hashes, and modpack commits. Shulker writes it and you commit it, and nothing in it is hand-edited. Its fields are described by [its JSON Schema](https://shulker.sh/schema/v1/lock.json), which editors read from the `$schema` line at the top of the file.

## Sides

A project has a client side, a server side, or both, and each is a build output, a client instance or a server directory. A side exists when its block does. `"client": {}` in `shulker.json` declares the client, `"server": {}` the server, and the block holds that side's settings, such as `client.options`, `client.memory` or `server.properties`. Each side builds into `build/<side>`, or in place, with `"build": "."`, which is what makes the project an [instance](#instances), so one project produces a client and a server from the same mod list. [`shulker build`](/docs/cli#shulker-build) builds every declared side, or the one you name. To play a server-only pack someone else publishes, pass `--assume-client` to `link`, `sync` or `export`, and Shulker builds a client from what both sides share.

Every mod has a side too, `client`, `server`, or `both`. Shulker reads it from the provider, then from the jar: Fabric's and Quilt's metadata name one, and a NeoForge or Forge jar, whose `mods.toml` names none, is `client` when it declares dependencies and every one is `side = "CLIENT"`, since the loader skips those on a server and the mod then fails there. Anything else is `both`. You can override it with `side` on the mod, and the lock's `sideFrom` records which of these decided. A side only gets mods for itself plus those marked `both`, so client-only mods like Iris never end up on a server. A dependency a jar declares for one side only is checked only on that side. Resource packs and shaders are always client-only, and a server build leaves them out.

A server build writes `eula.txt` once you have accepted the [Minecraft EULA](https://aka.ms/MinecraftEULA). The first [`shulker serve`](/docs/cli#shulker-serve) asks and records your answer in your Shulker config rather than the manifest, so no project asks again, and a manifest cannot accept it for you.

## Dependencies

Shulker reads each mod's dependencies from the jar's own metadata, not from the provider's page, and checks them for every side the project declares. `add`, `remove`, `update`, `lock` and `install` all run the check, so a mod whose dependency is missing or the wrong version fails there rather than in the game.

- Each side is checked against the mods its own build places. A client mod that needs a server-only mod fails, since the client build leaves that mod out, and a problem only one side has names the side.
- A problem you know is harmless is accepted with [`shulker ignore`](/docs/cli#shulker-ignore). The problem prints the exact command to run. The entry lands in [`ignore`](/docs/manifest#ignore) in `shulker.json` with a note saying why, and holds only while the jar declares that same range, so a new version that declares another brings the problem back.
- The loader's own dependency overrides apply first. On Fabric that is `config/fabric_loader_dependencies.json`, and on NeoForge for Minecraft 1.21.1 and later the `dependencyOverrides` table of `config/fml.toml`, each as the build lays it from your overrides. A dependency the file removes needs no ignore. Quilt's `config/quilt-loader-overrides.json` isn't applied, and a build that places one warns.
- [`shulker suggests`](/docs/cli#shulker-suggests) lists the mods your locked mods recommend that aren't installed, and `--optional` adds their optional dependencies.
- [`shulker check`](/docs/cli#shulker-check) runs the same check without writing anything, for CI. See [GitHub Actions](/docs/github-actions).

## Overrides

Overrides are folders of files copied into a build on top of the mods, such as configs, resource packs and scripts. The folders are fixed by convention, and each is used only if it exists. Later folders win, in this order, with features in name order, so a shared config can sit under a server-only one.

1. `overrides/` goes into both sides.
2. `client-overrides/` and `server-overrides/` go into one.
3. `<feature>-overrides/` (or the path the feature's `overrides` names) goes into a build while that feature is on.

Some files are generated instead of copied. `server.properties`, `options.txt`, and the whitelist, ops, and ban lists come from `shulker.json`, and a shader mod's `iris.properties` from the shader you locked.

### Templates

Files ending in `.tmpl` have `${name}` replaced with the side's [`variables`](/docs/manifest#variables) and are written without the suffix. `server.properties` and `client.options` values in the manifest expand the same variables. The built-in variables are always there as well.

| Variable | Comes From |
| --- | --- |
| `${project.name}`, `${project.displayName}`, `${project.version}` | The manifest of the project that owns the folder, so a pulled pack's files get that pack's own. `${project.displayName}` is the side's `name`, or the project's when the side has none. An export given `--version` uses it for the project's own `${project.version}` |
| `${minecraft.version}`, `${minecraft.dataVersion}` | The lock. `${minecraft.dataVersion}` is the number Minecraft stamps into worlds and `options.txt` |
| `${java.major}`, `${loader.type}`, `${loader.version}` | The lock |

A built-in with nothing to fill it, like `${project.version}` in a manifest with no `version`, is unset, and using it fails the build.

### Excluded Files

Folder metadata your operating system leaves behind (`.DS_Store`, `._*` files, `Thumbs.db`, `desktop.ini`) never goes into a build or an export, and the manifest's [`skipFiles`](/docs/manifest) leaves out more by glob.

## Features

A feature is a named, optional part of a pack, like `shaders`, that each player switches on or off for their own machine. [`features`](/docs/manifest#featuredecl) in `shulker.json` declares each one and whether it starts on, and a `requires` entry joins it with `feature`.

```json
"features": { "shaders": { "default": true } },
"requires": {
  "iris": { "feature": "shaders" },
  "sodium-extra": { "feature": "!shaders" }
}
```

- An entry gated on a feature ships only while the feature is on, and `!name` ships it only while the feature is off. A list of names is any-of, and every `!name` in it has to hold as well.
- A dependency that only gated-out mods need is left out with them. A gated-out mod that a mod still in the build requires is kept, and the build warns.
- A feature brings its own override folder, `<feature>-overrides/` unless its `overrides` names another, laid over the base folders while it is on. See [Overrides](#overrides).
- [`shulker feature on|off`](/docs/cli#shulker-feature-on-off) records your choice in `shulker.local.json` beside the manifest, which is per machine and kept out of git. `feature reset` goes back to the declared default, and `feature list` shows each feature with the mods it gates.
- `--with` and `--without` on `build`, `install`, `sync` and `export` decide a feature for one run, over both your choice and the default.
- A linked instance keeps its own choices, so `shulker feature on shaders -i <id>` turns a feature on for one instance and leaves the project alone.

### Operating Systems

An entry can also be held to an operating system with `os`, which takes `macos`, `windows` or `linux`, or `!name` to leave one out. It is decided where the build runs, and it combines with `feature`, so both have to hold.

```json
"requires": { "controlify": { "os": "!macos" } }
```

## Edits in the Build Directory

Each build records what it wrote in `.shulker/state.json` in the build directory, so the next build can tell your edits apart from its own files. That way config you change in-game survives.

- A file you edited is kept, as long as its source hasn't changed.
- If the file changed in both places, or a file Shulker didn't write is in the way, the build stops and lists it. Use `shulker build --force` to overwrite. A sync for a launch, from [`shulker play`](/docs/cli#shulker-play) or a launcher's pre-launch hook, is the one exception. It keeps your file, applies the rest and warns, so an update never holds the game back.
- Generated files like `server.properties` merge per key, so a key you edited in-game and a key you changed in `shulker.json` both apply. If both changed the same key, `shulker.json` wins and the build warns, unless the file is seeded.
- A `.properties` file in your overrides merges per key too. Shulker manages only the keys it lists, and leaves any others a mod writes alone. A file the mod wrote first gets those keys merged in instead of stopping the build. A file nothing merges into, because no other layer or Shulker changes a key in it, is placed exactly as written. List a path in the manifest's [`wholeFiles`](/docs/manifest) to copy it whole instead.
- A file the manifest's [`seedFiles`](/docs/manifest) lists is seeded: the pack supplies a starting copy and you own it after that. It is written when missing, deleted included, and follows the pack while you haven't changed it. Once both have changed it, every build keeps yours and warns, never stops. Delete it to take the pack's again, or `shulker build --force` for every seeded file. A file merged per key, like `options.txt` from `client.options` or a `.properties` override, is seeded key by key instead: a key you changed keeps your value while the rest follow the pack, and a key the pack drops goes only if you left it alone. A pulled modpack's own `seedFiles` covers the files it brings.

Use [`shulker diff`](/docs/cli#shulker-diff) to see what changed, and [`shulker pull`](/docs/cli#shulker-pull) to copy those edits back into your overrides or `shulker.json` so they're part of the project.

## Modpacks

A modpack is a `requires` entry that brings another pack's mods and overrides into this one. Its overrides sit beneath your own layers, and a mod you list in `shulker.json` yourself always wins over what a modpack provides. It comes from one of three places, and [`shulker modpack add`](/docs/cli#shulker-modpack-add-remove-list), or a bare `shulker add`, takes each.

### A Source

Another Shulker project at a local path, git URL, or manifest URL. A git repository holding several packs names the folder of the one you want with `path`. A raw manifest URL brings only the pack's `shulker.json` and `shulker.lock`, never its overrides or local files, so every command that reads one says so, and one naming a local `file` fails. Use the repository's git URL for a pack that has either.

- A **locked** modpack ships a `shulker.lock`. The exact versions it pins, dependencies included, are copied into your lock and marked with the modpack they came from, and its Minecraft and loader have to match yours exactly. When you set neither, yours are taken from it, and every locked modpack has to agree.
- A **floating** modpack has no lock, or was added with `--unlocked`. Its mods are resolved here alongside your own, and its versions only have to fit your ranges.

### A Pack Archive

A local `.mrpack` or CurseForge modpack zip, named by `file`. Its mods lock as the modpack's, found by hash on Modrinth or by project and file ID on CurseForge, and its override folders are laid before your own. The lock records the archive's bytes, so changing the file makes the lock out of date until `shulker lock` reads it again.

### A Hosted Modpack

A Modrinth or CurseForge modpack by its slug, like `shulker add cozy`. The newest version that fits your Minecraft and loader is locked, and its archive is read as a local one is. `pin`, `unpin`, `update` and `outdated` treat it like a mod, and `sync` never moves it.

## Instances

An instance is a project that builds in place, so its own folder is the game directory, living in a launcher's instance folder. The instance is a project in its own right, so `shulker add` there puts a mod on top of the pack and keeps it through every update.

### Linking

[`shulker link <launcher>`](/docs/cli#shulker-link) makes one for Prism Launcher, MultiMC, ATLauncher, GDLauncher, the Minecraft Launcher, or Shulker itself. It writes a `shulker.json` in the launcher's game directory that follows the pack you linked as a modpack, takes its Minecraft version, loader, features and overrides from it, and builds in place before `link` returns. The pack can be a project of your own, or someone else's git or manifest URL.

### Syncing

A linked instance syncs from its source before each launch. The launcher runs a small script Shulker keeps in the instance's `.shulker/` folder, and the Minecraft Launcher runs a shim in the profile's Java slot instead. Each sync records how it went in the instance's own [`.shulker/instance.json`](/docs/instance), which also holds the settings that decide how Shulker sets the instance up.

- [`shulker instances`](/docs/cli#shulker-instances) lists every instance under a short id, and `-i <id>` acts on one from anywhere.
- [`shulker unlink`](/docs/cli#shulker-unlink) stops syncing one while leaving its files.
- A directory you `sync --into` is a detached build instead, with no registry row and no hooks.

### Instances Shulker Owns

An instance Shulker owns is one it launches itself. [`shulker play`](/docs/cli#shulker-play) syncs it, fetches the game, its libraries and the loader into a shared store, and starts the game as an [account](/docs/cli#shulker-accounts) you signed in with `shulker accounts login`, an offline account, or one read from another launcher's files. Those instances share their worlds through [save groups](/docs/cli#shulker-saves), and `backup` and `restore` zip and put back the worlds of any instance.

### Instance Settings

Every instance has its own settings in [`.shulker/instance.json`](/docs/instance), which are per machine and never part of the pack. [`shulker instance set`](/docs/cli#shulker-instance) changes one, for the instance the current directory is or the one `-i` names.

```sh
shulker instance set memory 8G
shulker instance set window 1920x1080 -i smp
shulker instance set jvmArgs --literal '["-XX:+UseZGC"]'
```

- `memory`, `jvmArgs` and `window` apply in every launcher. Under `shulker play` they are the heap size, extra JVM arguments and window size of the launch, and for an instance in another launcher Shulker writes them into that launcher's own settings at each link, sync or repair. ATLauncher keeps one window size for every instance, so `window` there warns instead.
- A setting you leave unset leaves the other launcher's own value alone. Under `shulker play` it falls back to the `play.` default in your config, which [`shulker config set`](/docs/cli#shulker-config-set) sets for every instance at once, and for `memory` then to the pack's `client.memory`, and then to `4G`.
- `java` and `wrapper` point the launch at a Java of your own and a command to run it through, such as `gamemoderun`.
- `sandbox` runs the game out of reach of the rest of your home folder, in every launcher. It is experimental. See [The Sandbox](/docs/security#the-sandbox).
- `account` picks the account `shulker play` launches the instance as, over the default.
- `hooks.preLaunch` and `hooks.postExit` turn the sync before each launch, and the record of each run, on or off.

### The Pack in Its Launcher

A linked instance carries the pack's name and icon into its launcher. [`icon`](/docs/manifest#properties) in `shulker.json` names a PNG in the project, and each launcher shows it, fitted to the launcher's own shape. A sync replaces the launcher's image only when the pack's icon changes, so one you picked yourself stays until then. Exports carry the icon too.

### The Mod List Entry

A client build includes a small marker mod, which gives the pack its own entry in the in-game mod list, with its name, `description`, icon and [`links`](/docs/manifest#links).

- The entry shows what Shulker wants you to know about the instance. A Notices section names files gone from their provider and jars changed since Shulker placed them, and the last 30 days of syncs each get a section listing what they changed.
- The marker is also how an export of the build is recognised as this pack again.
- `"marker": false` in `shulker.json` leaves it out, and `shulker instance set marker false` does the same for one instance.

## History and Rollback

An instance keeps the states it was in before it changed, so an update that breaks the game can be undone.

- An entry is taken before anything is rewritten. `add`, `remove`, `update` and `lock` take one before they save `shulker.json` and `shulker.lock`, and an in-place build takes one before it writes over a file you changed.
- An entry holds the manifest, the lock, the whole `config` directory and every other file the build manages. Mod and pack files aren't copied, since the restored lock brings them back from the [cache](#cache).
- [`shulker history list`](/docs/cli#shulker-history-list) lists the entries, newest first, and `history show` says what restoring one would bring back, remove and change.
- [`shulker rollback`](/docs/cli#shulker-rollback) restores an entry and builds it in place. The current state is kept as an entry first, so a rollback can itself be rolled back.
- [`history`](/docs/manifest#properties) in `shulker.json` is how many entries `history prune` keeps, 5 by default. Nothing else deletes history, and a build over that number only warns. `-1` keeps every entry, and `0` takes none.

Only a project with a side that builds in place keeps history, which is every linked instance.

## Saves and Backups

History covers the pack's files, and backups cover the worlds.

### Save Groups

Every instance Shulker owns shares its worlds through a save group. Its `saves/` folder is a link to the group's folder, so every instance in a group lists the same worlds, and a new instance of another pack opens the worlds you already have. Each instance joins the `default` group.

- `shulker instance set savesGroup <name>` moves an instance to another group, and `none` keeps its worlds in the instance. Nothing is copied or merged, and leaving a group leaves its worlds there.
- Instances in other launchers keep their own worlds.
- [`shulker saves`](/docs/cli#shulker-saves) lists the groups, or one group's or instance's worlds and backups.

### Backups

[`shulker backup`](/docs/cli#shulker-backup) zips the worlds of a save group, an instance in any launcher, or a server, and [`shulker restore`](/docs/cli#shulker-restore) puts them back.

- An `update` or a `sync` that changes the mods backs the worlds up first, and keeps the newest five of those automatic backups. `play.saveBackups` in your config changes the number.
- A backup you take yourself is never deleted automatically. [`shulker saves prune`](/docs/cli#shulker-saves-prune) deletes backups, and always says how many it keeps.
- A restore backs up the worlds that are there first, so it can be undone, and replaces each world whole rather than merging files. `--world` restores only the worlds you name.
- A restore refuses to replace a world a running game or server has open. Quit to the title screen, or stop the server, first.

## Resource Packs and Shaders

Resource packs and shaders sit in `requires` beside your mods, and the provider's own project type decides which is which, so `shulker add fresh-animations` needs no `--type`.

### Filenames

Each is placed under its entry's `filename`. `shulker add` sets it to the provider's file name, the one other packs' `options.txt` and Paxi load orders already name, and then holds it. Minecraft and Iris both enable packs by literal file name, so a stable name is what keeps a pack you turned on from quietly switching off the next time it updates, even though the provider's file name changes with every version.

- An entry without `filename` is placed under its `requires` key, `resourcepacks/<key>.zip` or `shaderpacks/<key>.zip`.
- Set `filename` to a name of your own, such as the `Chat Reporting Helper.zip` players of another modpack already have enabled. Change it later and the build moves the pack's entry in your enabled list along with it.

### Shaders

A shader starts enabled only when `client.shader` names it; with none named, the build selects no shader, since a shader is a performance choice. `"shader": ""` clears a selection the pack's overrides ship. It is written into its shader mod's own config, `config/iris.properties`, or `config/oculus.properties` on Forge. Shulker writes only `shaderPack` and `enableShaders` there, so the rest of your shader settings survive a rebuild.

- Each build looks for Iris or Oculus among the mods it placed, so a shader mod behind a feature only enables a shader in the builds that have it.
- The named shader goes to the first of those mods that can load it, going by the shader mods the provider tagged it for. A shader added from a file loads in either.
- The other shaders are placed but off, with no warning. Only a shader nothing in the build can load gets a line in the build report, with `shulker add iris` to fix it.
- A shader that ships vanilla core shaders needs no shader mod at all. It goes in `resourcepacks/` and is enabled like a resource pack.

Shulker knows Iris, Oculus and Canvas by their jar ids. A fork under another id is marked in [`integrations`](/docs/manifest#integrations), which replaces the ids Shulker looks for, so list the original beside it. An empty list turns a shader mod off:

```json
"integrations": { "iris": ["iris", "iris_fork"] }
```

### The Enabled List

The enabled list in `options.txt` works differently, because it is in priority order and yours to arrange. `client.resourcePacks` names the packs that start on, top first, by their keys, along with the game's own `programmer_art` and `high_contrast`; a placed pack it leaves out is off. Without it, Shulker seeds the list once, on the first build, with every placed pack, when the line is missing or still Minecraft's own `["vanilla"]`, and then leaves it alone.

- With `client.resourcePacks`, the build writes the list again whenever you change it, unless the player has changed the list in game since: then theirs stays, and the build report says so. The shader named in `client.shader` works the same way. `shulker build --force` writes yours.
- Without it, a pack you add later is placed but not enabled. Turn it on in game, and Shulker won't reorder what you chose. `shulker build --force` seeds the list again.
- An `options.txt` in your overrides that sets its own `resourcePacks` list is never seeded over, unless `client.resourcePacks` is set. That list is the pack's own, and the build names each pack entry in it that no pack is placed under, such as a pack renamed since the list was written. `client.options.resourcePacks` sets the raw list too, for a list that names packs `client.resourcePacks` can't, but not together with it.
- Importing a pack moves the list its `options.txt` ships, and the shader its `iris.properties` or `oculus.properties` selects, into `client.resourcePacks` and `client.shader`, unless the list names a pack a mod provides.
- Before Minecraft 1.13 the game names a pack by its bare file name rather than `file/<name>`, and the seed and the checks follow that.

## Datapacks

A datapack sits in `requires` too, with `"type": "datapack"`. Modrinth files datapacks as mods, so a project whose only files are datapacks adds as one on its own, and a mod that also ships a datapack, like Terralith, needs `shulker datapack add` to get the datapack.

Vanilla loads datapacks only per world, so a datapack for every world needs a global datapack mod. Each build places a datapack in the folder of one it finds among the mods it placed.

- `config/paxi/datapacks/` for Paxi.
- `config/openloader/data/` before Minecraft 1.21, or `config/openloader/packs/` from it, for Open Loader.
- A server without one places it in its world's own `datapacks/` folder, the world `level-name` names, which the game loads with no mod at all. That one folder inside the world is the build's to fill, so a datapack edited or replaced there, as by restoring an old backup, is a conflict until `--force`.
- A client without one places it in `datapacks/`, which only some global datapack mods read, and says so.

Shulker knows Paxi and Open Loader by their jar ids. Mark a fork under another id in [`integrations`](/docs/manifest#integrations), listing the original beside it, or turn one off with an empty list:

```json
"integrations": { "paxi": ["paxi", "paxi_fork"] }
```

A datapack is placed on both sides unless its `side` says otherwise, since a singleplayer world runs its server inside the client. Load order isn't Shulker's. Ship Paxi's `datapack_load_order.json` as an override, naming each datapack by its file name, which is `<key>.zip` unless `filename` says otherwise.

## Providers

Mods, resource packs, shaders and datapacks are resolved from Modrinth or CurseForge. By default Modrinth is tried first, then CurseForge. Set [`providers`](/docs/manifest#properties) to change the order or use only one, or set `provider` on a single mod.

CurseForge needs an API key. Release builds include one, so it works without setup. To use your own, set `SHULKER_CURSEFORGE_KEY` or run [`shulker config set curseforge.key <key>`](/docs/cli#shulker-config-set). Your key always takes priority and is never replaced. If CurseForge rejects the included key, Shulker fetches a new one from shulker.sh, saves it in its cache, and retries once. If that fails too, it asks you to set your own key or report an issue.

## Cache

Every file Shulker downloads is stored once, by hash, in a cache shared by all your projects, `~/Library/Caches/shulker` on macOS, `~/.cache/shulker` on Linux and `%LOCALAPPDATA%\shulker` on Windows, or wherever `SHULKER_CACHE` points. A build places files out of it, so a mod ten instances use is downloaded once, and installing a pack you have built before needs no network at all.

Nothing Shulker placed is deleted without its bytes reaching the cache first. That is what makes [`shulker rollback`](/docs/cli#shulker-rollback) work offline. A history entry leaves mod and pack files out and relies on the cache to put them back.

[`shulker cache prune`](/docs/cli#shulker-cache-prune) removes what nothing references. What counts as a reference is every registered instance and the project you run it in, which means each one's `shulker.lock`, the lock of every history entry it keeps, and the modpack checkouts and offline sync fallbacks its sources need. A registered folder that is gone is skipped, and one whose lock can't be read stops the prune instead. Files you downloaded by hand are kept unless you pass `--manual`, since nothing can fetch them again. Your CurseForge key and the Java runtimes Shulker manages are never touched. [`shulker cache info`](/docs/cli#shulker-cache-info) shows what it would free before you run it.
