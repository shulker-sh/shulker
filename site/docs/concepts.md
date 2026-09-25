---
description: How manifests, locks, sides, overrides, build edits, modpacks, instances, and providers fit together.
---

# Concepts

## Manifest

`shulker.json` declares the Minecraft version, loader, sides, mods, and modpacks. See the [manifest reference](/docs/manifest).

## Lock

`shulker.lock` records the exact resolved versions, hashes, and modpack commits. Shulker writes it and you commit it; nothing in it is hand-edited. Its fields are described by [its JSON Schema](https://shulker.sh/schema/v1/lock.json), which editors read from the `$schema` line at the top of the file.

## Sides

A project has a client side, a server side, or both, and each is a build output: a client instance or a server directory. A side exists when its block does: `"client": {}` in `shulker.json` declares the client, `"server": {}` the server, and the block holds that side's settings, such as `client.options` or `server.properties`. Each side builds into `build/<side>`, or in place, with `"build": "."`, which is what makes the project an [instance](#instances), so one project produces a client and a server from the same mod list. [`shulker build`](/docs/cli#shulker-build) builds every declared side, or the one you name. To play a server-only pack someone else publishes, pass `--assume-client` to `link`, `sync` or `export`, and shulker builds a client from what both sides share.

Every mod has a side too: `client`, `server`, or `both`. Shulker reads it from the provider, and you can override it with `side` on the mod. A side only gets mods for itself plus those marked `both`, so client-only mods like Iris never end up on a server. Resource packs and shaders are always client-only, and a server build leaves them out.

## Overrides

Overrides are folders of files copied into a build on top of the mods: configs, resource packs, scripts. The folders are fixed by convention, and each is used only if it exists: `overrides/` goes into both sides, `client-overrides/` and `server-overrides/` into one, and `<feature>-overrides/` (or the path the feature's `overrides` names) into a build while that feature is on. Later folders win, in that order, with features in name order, so a shared config can sit under a server-only one. Files ending in `.tmpl` have `${name}` replaced with the side's [`variables`](/docs/manifest#variables) and are written without the suffix. The built-in variables are always there as well. `${project.name}`, `${project.displayName}` (the side's `name`, or the project's when the side has none) and `${project.version}` come from the manifest of the project that owns the folder, so a pulled pack's files get that pack's own. `${minecraft.version}`, `${minecraft.dataVersion}` (the number Minecraft stamps into worlds and `options.txt`), `${java.major}`, `${loader.type}` and `${loader.version}` come from the lock. An export given `--version` uses it for the project's own `${project.version}`. A built-in with nothing to fill it, like `${project.version}` in a manifest with no `version`, is unset, and using it fails the build. `server.properties` and `client.options` values in the manifest expand the same variables. Folder metadata your operating system leaves behind (`.DS_Store`, `._*` files, `Thumbs.db`, `desktop.ini`) never goes into a build or an export, and the manifest's [`skipFiles`](/docs/manifest) leaves out more by glob.

Some files are generated instead of copied: `server.properties`, `options.txt`, and the whitelist, ops, and ban lists from `shulker.json`, and a shader mod's `iris.properties` from the shader you locked.

## Edits in the build directory

Each build records what it wrote in `.shulker/state.json` in the build directory, so the next build can tell your edits apart from its own files. That way config you change in-game survives:

- A file you edited is kept, as long as its source hasn't changed.
- If the file changed in both places, or a file shulker didn't write is in the way, the build stops and lists it. Use `shulker build --force` to overwrite. A sync for a launch, from [`shulker play`](/docs/cli#shulker-play) or a launcher's pre-launch hook, is the one exception: it keeps your file, applies the rest and warns, so an update never holds the game back.
- Generated files like `server.properties` merge per key, so a key you edited in-game and a key you changed in `shulker.json` both apply. If both changed the same key, `shulker.json` wins and the build warns.
- A `.properties` file in your overrides merges per key too. shulker manages only the keys it lists, and leaves any others a mod writes alone. A file the mod wrote first gets those keys merged in instead of stopping the build. A file nothing merges into, because no other layer or shulker changes a key in it, is placed exactly as written. List a path in the manifest's [`wholeFiles`](/docs/manifest) to copy it whole instead.

Use [`shulker diff`](/docs/cli#shulker-diff) to see what changed, and [`shulker pull`](/docs/cli#shulker-pull) to copy those edits back into your overrides or `shulker.json` so they're part of the project.

## Modpacks

A modpack is a `requires` entry that brings another pack's mods and overrides into this one. Its overrides sit beneath your own layers, and a mod you list in `shulker.json` yourself always wins over what a modpack provides. It comes from one of three places, and [`shulker modpack add`](/docs/cli#shulker-modpack-add-remove-list), or a bare `shulker add`, takes each:

- **A source**: another shulker project at a local path, git URL, or manifest URL. One that ships a `shulker.lock` is **locked**: the exact versions it pins, dependencies included, are copied into your lock and marked with the modpack they came from, and its Minecraft and loader have to match yours exactly — or, when you set neither, yours are taken from it, and every locked modpack has to agree. One with no lock, or added with `--unlocked`, is **floating**: its mods are resolved here alongside your own, and its versions only have to fit your ranges. A git repository holding several packs names the folder of the one you want with `path`. A raw manifest URL brings only the pack's `shulker.json` and `shulker.lock`, never its overrides or local files, so every command that reads one says so, and one naming a local `file` fails; use the repository's git URL for a pack that has either.
- **A pack archive**: a local `.mrpack` or CurseForge modpack zip, named by `file`. Its mods lock as the modpack's, found by hash on Modrinth or by project and file ID on CurseForge, and its override folders are laid before your own. The lock records the archive's bytes, so changing the file makes the lock out of date until `shulker lock` reads it again.
- **A hosted modpack**: a Modrinth or CurseForge modpack by its slug, like `shulker add cozy`. The newest version that fits your Minecraft and loader is locked, and its archive is read as a local one is. `pin`, `unpin`, `update` and `outdated` treat it like a mod, and `sync` never moves it.

## Instances

An instance is a project that builds in place, so its own folder is the game directory, living in a launcher's instance folder. [`shulker link <launcher>`](/docs/cli#shulker-link) makes one for Prism Launcher, MultiMC, ATLauncher, GDLauncher, the official launcher, or shulker itself: a `shulker.json` in the launcher's game directory that follows the pack you linked as a modpack, takes its Minecraft version, loader, features and overrides from it, and builds in place before `link` returns. The pack can be a project of your own, or someone else's git or manifest URL. The instance is a project in its own right, so `shulker add` there puts a mod on top of the pack and keeps it through every update.

A linked instance syncs from its source before each launch: the launcher runs a small script shulker keeps in the instance's `.shulker/` folder, and the official launcher runs a shim in the profile's Java slot instead. Each sync records how it went in the instance's own [`.shulker/instance.json`](/docs/instance), which also holds the settings that decide how shulker sets the instance up. [`shulker instances`](/docs/cli#shulker-instances) lists every instance under a short id, `-i <id>` acts on one from anywhere, and [`shulker unlink`](/docs/cli#shulker-unlink) stops syncing one while leaving its files. A directory you `sync --into` is a detached build instead: no registry row and no hooks.

An instance shulker owns is one it launches itself. [`shulker play`](/docs/cli#shulker-play) syncs it, fetches the game, its libraries and the loader into a shared store, and starts the game as an [account](/docs/cli#shulker-accounts) you signed in with `shulker accounts login`, an offline account, or one borrowed from another launcher's files. Those instances share their worlds through [save groups](/docs/cli#shulker-saves), and `backup` and `restore` zip and put back the worlds of any instance.

## Resource packs and shaders

Resource packs and shaders sit in `requires` beside your mods, and the provider's own project type decides which is which, so `shulker add fresh-animations` needs no `--type`.

Each is placed under its entry's `filename`. `shulker add` sets it to the provider's file name, the one other packs' `options.txt` and Paxi load orders already name, and then holds it: Minecraft and Iris both enable packs by literal file name, so a stable name is what keeps a pack you turned on from quietly switching off the next time it updates, even though the provider's file name changes with every version. An entry without `filename` is placed under its `requires` key, `resourcepacks/<key>.zip` or `shaderpacks/<key>.zip`. Set `filename` to a name of your own, such as the `Chat Reporting Helper.zip` players of another modpack already have enabled; change it later and the build moves the pack's entry in your enabled list along with it.

A shader is enabled through its shader mod's own config, `config/iris.properties`, or `config/oculus.properties` on Forge. Each build looks for Iris or Oculus among the mods it placed, so a shader mod behind a feature only enables a shader in the builds that have it, and it enables the first shader that mod can load, going by the shader mods the provider tagged it for. A shader added from a file loads in either. Every other shader the build placed gets a line in the build report: turn it on in game, or in Canvas's own menu, which has no config file for shulker to write, or, when nothing in the build can load it, add a shader mod with `shulker add iris`. Shulker writes only `shaderPack` and `enableShaders` there, so the rest of your shader settings survive a rebuild. A shader that ships vanilla core shaders needs no shader mod at all: it goes in `resourcepacks/` and is enabled like a resource pack.

The enabled list in `options.txt` works differently, because it is in priority order and yours to arrange. Shulker seeds it once — on the first build, when the line is missing or still Minecraft's own `["vanilla"]` — and then leaves it alone. A pack you add later is placed but not enabled: turn it on in game, and shulker won't reorder what you chose. `shulker build --force` seeds the list again. An `options.txt` in your overrides that sets its own `resourcePacks` list is never seeded over: that list is the pack's own, and the build names each `file/` entry in it that no pack is placed under, such as a pack renamed since the list was written.

## Datapacks

A datapack sits in `requires` too, with `"type": "datapack"`. Modrinth files datapacks as mods, so a project whose only files are datapacks adds as one on its own, and a mod that also ships a datapack, like Terralith, needs `shulker datapack add` to get the datapack.

Vanilla loads datapacks only per world, so a datapack for every world needs a global datapack mod. Each build places a datapack in the folder of one it finds among the mods it placed: `config/paxi/datapacks/` for Paxi, and `config/openloader/data/` before Minecraft 1.21 or `config/openloader/packs/` from it for Open Loader. A server without one places it in its world's own `datapacks/` folder, the world `level-name` names, which the game loads with no mod at all. That one folder inside the world is the build's to fill: a datapack edited or replaced there, as by restoring an old backup, is a conflict until `--force`. A client without one places it in `datapacks/`, which only some global datapack mods read, and says so.

A datapack is placed on both sides unless its `side` says otherwise, since a singleplayer world runs its server inside the client. Load order isn't shulker's: ship Paxi's `datapack_load_order.json` as an override, naming each datapack by its file name, which is `<key>.zip` unless `filename` says otherwise.

## Providers

Mods, resource packs, shaders and datapacks are resolved from Modrinth or CurseForge. By default Modrinth is tried first, then CurseForge. Set [`providers`](/docs/manifest#properties) to change the order or use only one, or set `provider` on a single mod.

CurseForge needs an API key. Release builds include one, so it works without setup. To use your own, set `SHULKER_CURSEFORGE_KEY` or run [`shulker config set curseforge.key <key>`](/docs/cli#shulker-config-set); your key always takes priority and is never replaced. If CurseForge rejects the included key, shulker fetches a new one from shulker.sh, saves it in its cache, and retries once. If that fails too, it asks you to set your own key or report an issue.

## Cache

Every file shulker downloads is stored once, by hash, in a cache shared by all your projects: `~/Library/Caches/shulker` on macOS, or wherever `SHULKER_CACHE` points. A build places files out of it, so a mod ten instances use is downloaded once, and installing a pack you have built before needs no network at all.

Nothing shulker placed is deleted without its bytes reaching the cache first. That is what makes [`shulker rollback`](/docs/cli#shulker-rollback) work offline: a history entry leaves mod and pack files out and relies on the cache to put them back.

[`shulker cache prune`](/docs/cli#shulker-cache-prune) removes what nothing references. What counts as a reference is every registered instance and the project you run it in — each one's `shulker.lock`, the lock of every history entry it keeps, and the modpack checkouts and offline sync fallbacks its sources need. A registered folder that is gone is skipped; one whose lock can't be read stops the prune instead. Your CurseForge key and the Java runtimes shulker manages are never touched. [`shulker cache info`](/docs/cli#shulker-cache-info) shows what it would free before you run it.
