---
description: Every shulker command with its flags and examples.
outline: [2, 3]
---

# CLI Reference

| Command | Description |
| --- | --- |
| [`shulker init`](#shulker-init) | Create shulker.json and a lock in the current directory, asking what is not given |
| [`shulker create`](#shulker-create) | Create shulker.json and a lock in the current directory without asking |
| [`shulker add <mod>...`](#shulker-add) | Add mods or modpacks to the manifest and lock |
| [`shulker search [words...]`](#shulker-search) | Search the providers for projects to add |
| [`shulker remove <mod>...`](#shulker-remove) | Remove mods or modpacks from the manifest and prune what only they provided |
| [`shulker lock [key...]`](#shulker-lock) | Bring the lock in line with shulker.json without upgrading, or look entries up again at their locked version |
| [`shulker check [lock\|files\|deps\|server]...`](#shulker-check) | Fail when the lock is stale, a locked file can't be fetched, or a mod's dependencies aren't met |
| [`shulker audit [key...]`](#shulker-audit) | Report lock entries and files that deserve a closer look |
| [`shulker audit jar <entry>`](#shulker-audit-jar) | Show what a jar declares and holds |
| [`shulker audit file <entry> <path>`](#shulker-audit-file) | Print one file from inside a jar |
| [`shulker match [file...]`](#shulker-match) | Lock override jars and packs that Modrinth or CurseForge host |
| [`shulker update [mod...]`](#shulker-update) | Update mods to the newest compatible version |
| [`shulker outdated [mod...]`](#shulker-outdated) | Show mods with a newer compatible version |
| [`shulker suggests`](#shulker-suggests) | List mods that locked mods recommend and that aren't installed |
| [`shulker pin <mod> [version\|url]`](#shulker-pin) | Pin a mod to a provider version id or URL |
| [`shulker unpin <mod>`](#shulker-unpin) | Remove a mod's pin and re-resolve it |
| [`shulker ignore <mod> <on>`](#shulker-ignore) | Record that a dependency problem is safe to ignore |
| [`shulker unignore <mod> <on>`](#shulker-unignore) | Drop an ignored dependency problem |
| [`shulker set <path> <value>`](#shulker-set) | Set a field in shulker.json |
| [`shulker unset <path>`](#shulker-unset) | Remove a field from shulker.json |
| [`shulker get [path]`](#shulker-get) | Print a field of shulker.json, or all of it |
| [`shulker config get [key]`](#shulker-config-get) | Print a key of config.json, or all of it |
| [`shulker config set <key> <value>`](#shulker-config-set) | Set a key in config.json |
| [`shulker config unset <key>`](#shulker-config-unset) | Remove a key from config.json |
| [`shulker config path`](#shulker-config-path) | Print where config.json is |
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
| [`shulker link`](#shulker-link) | Ask which launcher to link, then link it |
| [`shulker link shulker [source]`](#shulker-link-shulker) | Create an instance shulker owns and launches itself |
| [`shulker link atlauncher [source]`](#shulker-link-atlauncher) | Create an ATLauncher instance for the client build |
| [`shulker link gdlauncher [source]`](#shulker-link-gdlauncher) | Create a GDLauncher instance for the client build |
| [`shulker link mojang [source]`](#shulker-link-mojang) | Add an official launcher profile that follows a pack |
| [`shulker link prism [source]`](#shulker-link-prism) | Create a Prism Launcher instance for the client build |
| [`shulker link multimc [source]`](#shulker-link-multimc) | Create a MultiMC instance for the client build |
| [`shulker sync [source]`](#shulker-sync) | Download and build one side of a project into a directory, or update a linked one |
| [`shulker instances`](#shulker-instances) | List the instances shulker keeps in sync |
| [`shulker instances repair`](#shulker-instances-repair) | Register instances shulker has lost track of and write any missing instance files |
| [`shulker unlink <name>`](#shulker-unlink) | Stop syncing a linked instance or synced directory, keeping its files |
| [`shulker instance get [path]`](#shulker-instance-get) | Print a setting in effect and the default behind it, or every setting |
| [`shulker instance set <path> <value>`](#shulker-instance-set) | Set a setting in this instance, over the default |
| [`shulker instance unset <path>`](#shulker-instance-unset) | Remove a setting from this instance, back to the default |
| [`shulker instance edit`](#shulker-instance-edit) | Open this instance's instance.json in your editor |
| [`shulker instance dump`](#shulker-instance-dump) | Print where the running game's main and render threads are, from a thread dump |
| [`shulker instance log`](#shulker-instance-log) | Print the game's output from the latest run |
| [`shulker saves`](#shulker-saves) | Show save groups, or one group's or instance's worlds and backups |
| [`shulker saves prune`](#shulker-saves-prune) | Delete all but the newest backups of a save group or instance |
| [`shulker backup`](#shulker-backup) | Zip an instance's or save group's worlds into its backups |
| [`shulker restore [n]`](#shulker-restore) | Put a backup's worlds back, taking a backup of the worlds there first |
| [`shulker list`](#shulker-list) | List everything in `requires` with its locked version |
| [`shulker mod add\|remove\|list`](#shulker-mod-add-remove-list) | The plain verbs with `--type mod` |
| [`shulker modpack add\|remove\|list`](#shulker-modpack-add-remove-list) | Manage modpacks whose mods and overrides merge into this project |
| [`shulker resourcepack add\|remove\|list`](#shulker-resourcepack-add-remove-list) | The plain verbs with `--type resourcepack` |
| [`shulker shader add\|remove\|list`](#shulker-shader-add-remove-list) | The plain verbs with `--type shader` |
| [`shulker datapack add\|remove\|list`](#shulker-datapack-add-remove-list) | The plain verbs with `--type datapack` |
| [`shulker player [name\|uuid]...`](#shulker-player) | Check player names and uuids against Mojang and the lock |
| [`shulker accounts`](#shulker-accounts) | List the accounts shulker can play with |
| [`shulker accounts login`](#shulker-accounts-login) | Sign in to a Microsoft account |
| [`shulker accounts logout [name]`](#shulker-accounts-logout) | Sign a Microsoft account out |
| [`shulker accounts add <name>`](#shulker-accounts-add) | Create an offline account |
| [`shulker accounts remove <name>`](#shulker-accounts-remove) | Delete an offline account |
| [`shulker accounts refresh [name...]`](#shulker-accounts-refresh) | Renew the accounts shulker signed in |
| [`shulker accounts use <name>`](#shulker-accounts-use) | Switch the default account |
| [`shulker accounts stores`](#shulker-accounts-stores) | List the launchers shulker reads accounts from |
| [`shulker accounts stores add\|remove <launcher>`](#shulker-accounts-stores-add-remove) | Read accounts from another launcher, or stop |
| [`shulker accounts stores set <launcher...>`](#shulker-accounts-stores-set) | Replace the list, in the order given |
| [`shulker import <modpack>`](#shulker-import) | Create a project from a modpack file, URL, slug or shulker source |
| [`shulker export mrpack [source]`](#shulker-export-mrpack) | Export a Modrinth modpack |
| [`shulker export curseforge [source]`](#shulker-export-curseforge) | Export a CurseForge modpack |
| [`shulker docs [topic]...`](#shulker-docs) | Print shulker's documentation |
| [`shulker cache info`](#shulker-cache-info) | Show the cache's size and how much prune would free |
| [`shulker cache verify`](#shulker-cache-verify) | Check every cached file against its hash and its provider |
| [`shulker cache prune`](#shulker-cache-prune) | Remove cached files no instance or project references |
| [`shulker security`](#shulker-security) | Explain how shulker keeps bad files off your machine |
| [`shulker log`](#shulker-log) | Show what shulker did, from its log |
| [`shulker version`](#shulker-version) | Print the shulker version |
| [`shulker self update`](#shulker-self-update) | Update shulker to the latest release |
| [`shulker self uninstall`](#shulker-self-uninstall) | Take shulker out of every launcher it hooked, then remove the binary |
| [`shulker completion bash`](#shulker-completion-bash) | Print the bash completion script |
| [`shulker completion zsh`](#shulker-completion-zsh) | Print the zsh completion script |
| [`shulker completion fish`](#shulker-completion-fish) | Print the fish completion script |
| [`shulker completion powershell`](#shulker-completion-powershell) | Print the PowerShell completion script |

## Global flags

These work with every command.

| Flag | Description |
| --- | --- |
| `--json` | Print machine-readable JSON, including errors; see [JSON output](#json-output) |
| `--no-input` | Ask nothing: every prompt takes its default, and a required value left unset is a usage error naming the flag that supplies it. Output that isn't going to a terminal implies it, and so does `--json` |
| `--no-color` | Print without colour. Setting `NO_COLOR` or `TERM=dumb` does the same, and colour is off whenever the output is not a terminal |
| `--ascii` | Print with ASCII glyphs (`*`, `x`, `|-`, `->`, `>>`) in place of `✔`, `✘`, `├─`, `⟶`, and `»` |
| `--annotations` | Also print each error and warning to stderr as a GitHub Actions workflow command, `::error title=<headline> (<code>)::<item>` for each of an error's items (or `::error title=<code>::<headline>` for one without) and `::warning::<warning>`, which the runner shows as annotations on the run. On by default when `GITHUB_ACTIONS=true`. Works with `--json`, whose output stays on stdout |
| `--no-annotations` | Print no GitHub Actions annotations, even when `GITHUB_ACTIONS=true` |

## Project and instance flags

These say which directory a command acts on, so only the commands that act on one take them. Anywhere else they are an unknown flag.

| Flag | Description |
| --- | --- |
| `-C, --dir <path>` | Project directory (default: current directory). It has to name a directory that exists, or it is a usage error before the command runs; `init`, `create` and `import` take one that doesn't exist yet, since they create the project there. An empty `-C ""` is the current directory |
| `-i, --instance <id>` | Act on a registered instance instead of a project directory, by id, name, or directory; `--id` is accepted as an alias. Can't be combined with `-C`. [`shulker instances`](#shulker-instances) lists them |

- Both: [`shulker add`](#shulker-add), [`shulker remove`](#shulker-remove), [`shulker list`](#shulker-list), [`shulker search`](#shulker-search), [`shulker lock`](#shulker-lock), [`shulker check`](#shulker-check), [`shulker audit`](#shulker-audit), [`shulker audit jar`](#shulker-audit-jar), [`shulker audit file`](#shulker-audit-file), [`shulker match`](#shulker-match), [`shulker update`](#shulker-update), [`shulker outdated`](#shulker-outdated), [`shulker suggests`](#shulker-suggests), [`shulker pin`](#shulker-pin), [`shulker unpin`](#shulker-unpin), [`shulker ignore`](#shulker-ignore), [`shulker unignore`](#shulker-unignore), [`shulker set`](#shulker-set), [`shulker unset`](#shulker-unset), [`shulker get`](#shulker-get), [`shulker export mrpack`](#shulker-export-mrpack), [`shulker export curseforge`](#shulker-export-curseforge), [`shulker feature on|off`](#shulker-feature-on-off), [`shulker feature reset`](#shulker-feature-reset), [`shulker feature list`](#shulker-feature-list), [`shulker install`](#shulker-install), [`shulker build`](#shulker-build), [`shulker diff`](#shulker-diff), [`shulker pull`](#shulker-pull), [`shulker history list|show|prune`](#shulker-history-list), [`shulker rollback`](#shulker-rollback), [`shulker play`](#shulker-play), [`shulker serve`](#shulker-serve), [`shulker link`](#shulker-link), [`shulker link shulker|atlauncher|gdlauncher|mojang|prism|multimc`](#shulker-link), [`shulker sync`](#shulker-sync), [`shulker instance get|set|unset|edit|dump|log`](#shulker-instance), [`shulker unlink`](#shulker-unlink), [`shulker saves`](#shulker-saves), [`shulker saves prune`](#shulker-saves-prune), [`shulker backup`](#shulker-backup), [`shulker restore`](#shulker-restore), [`shulker hook pre-launch|post-exit|wrap`](#shulker-hook-pre-launch), [`shulker mod add|remove|list`](#shulker-mod-add-remove-list), [`shulker modpack add|remove|list`](#shulker-modpack-add-remove-list), [`shulker resourcepack add|remove|list`](#shulker-resourcepack-add-remove-list), [`shulker shader add|remove|list`](#shulker-shader-add-remove-list), [`shulker datapack add|remove|list`](#shulker-datapack-add-remove-list), [`shulker player`](#shulker-player)
- `-C` only: [`shulker init`](#shulker-init), [`shulker create`](#shulker-create), [`shulker import`](#shulker-import), [`shulker cache info`](#shulker-cache-info), [`shulker cache verify`](#shulker-cache-verify), [`shulker cache prune`](#shulker-cache-prune)
- `-i` only: [`shulker log`](#shulker-log)

## Projects

### `shulker init`

Create `shulker.json` and `shulker.lock` in the current directory. On a terminal it asks six questions, in order: what you are making, which Minecraft version, whether to add mods, which mod loader, which version of it, and whether to start from an existing pack. Each question is skipped by the flag that answers it, and every answer starts on the default that flag has, so taking all six as they come creates what [`shulker create`](#shulker-create) creates. Answering *from an existing pack* asks for a source and adds it as a modpack, exactly as [`shulker add <source> --type modpack`](#shulker-add) would.

Under [`--no-input`](#global-flags) — which a script gets without asking for it, since output that isn't going to a terminal implies it — nothing is asked and every answer is its default: the latest release, no loader, the client side, and no pack. That is `shulker create`, which is the spelling for a script.

```sh
shulker init
shulker init --fabric
shulker init --name my-server --minecraft 1.21.1 --loader neoforge --side server
```

| Flag | Description |
| --- | --- |
| `--name <name>` | Project name (default: directory name) |
| `--minecraft <version>` | Minecraft version or range (default: latest release) |
| `--loader <loader>` | Mod loader: `none` (the default, vanilla Minecraft), `fabric`, `quilt`, `neoforge`, `forge` |
| `--fabric`, `--quilt`, `--neoforge`, `--forge` | The same as `--loader` naming that loader. Two of them together, or one beside a `--loader` that names another, is a usage error |
| `--loader-version <range>` | Loader version range (default: `*`); needs a loader |
| `--side <side>` | Side to declare: `client` or `server` |
| `--client`, `--server` | The same as `--side` naming that side. Both together, or one beside a `--side` that names the other, is a usage error |

### `shulker create`

Create `shulker.json` and `shulker.lock` in the current directory without asking anything, on a terminal or off one. Every choice no flag makes takes its default: the latest Minecraft release, no loader, the latest version of the loader named, the client side, and no pack to start from. So `shulker create --fabric` alone makes a Fabric client on the latest release and the latest Fabric. The flags are [`shulker init`](#shulker-init)'s, and the project is the one `init` makes with the same answers.

```sh
shulker create
shulker create --fabric --minecraft 26.2
shulker create --name my-server --minecraft 1.21.1 --neoforge --side server
```

| Flag | Description |
| --- | --- |
| `--name <name>` | Project name (default: directory name) |
| `--minecraft <version>` | Minecraft version or range (default: latest release) |
| `--loader <loader>` | Mod loader: `none` (the default, vanilla Minecraft), `fabric`, `quilt`, `neoforge`, `forge` |
| `--fabric`, `--quilt`, `--neoforge`, `--forge` | The same as `--loader` naming that loader. Two of them together, or one beside a `--loader` that names another, is a usage error |
| `--loader-version <range>` | Loader version range (default: `*`); needs a loader |
| `--side <side>` | Side to declare: `client` or `server` |
| `--client`, `--server` | The same as `--side` naming that side. Both together, or one beside a `--side` that names the other, is a usage error |

### `shulker import`

Create a project from a modpack, or merge one into the project. The argument is read in this order:

- **A modpack the project requires**, by its key. Its entries become the project's own: every lock entry it provides loses its `modpack` tag and gains a `requires` entry, as `add --with-deps` lists a mod a modpack pinned, and its override files, feature folders and blocks merge as below. Then its `requires` entry and its `modpacks` lock section go, so `update` moves its mods like any other and `remove` takes them out. A locked pack is read as `build` reads it, from the cache, its pinned commit or its cached manifest, so this works offline wherever `build` does; a floating one is taken as it resolves now. Where two packs provide one key, the inlined pack's entry becomes the project's and wins. `--type modpack` insists on this reading and fails with `usage` for anything else.
- **A path**: an argument with a `/`, one starting with `.` or `~`, or one ending in `.zip` or `.mrpack`. A bare word is a slug even where a folder of that name exists. A relative path is read from the current folder, never `-C`, which names where the import goes; the `-C` folder itself is refused with `usage`, and a path that isn't there fails with `file-not-found`. A file is a modpack archive, its kind read from what it holds, whatever its name: a `modrinth.index.json` makes it a Modrinth pack, a CurseForge `manifest.json` of type `minecraftModpack` a CurseForge one, and anything else fails with `archive-not-modpack`. A folder is a shulker source.
- **A URL.** An `http(s)` URL is downloaded and, when it holds a modpack archive, kept in the cache at its sha512 and read as one. One that holds anything else, or nothing, and a `git@`, `ssh://`, `git://` or `file://` URL, is a git source, or a raw manifest source when it ends in `.json`, as `modpack add` reads one. A raw manifest source gets the warning that its overrides didn't come with it. A URL whose name ends in `.mrpack` or `.zip` is taken for an archive: a download that fails fails with `modpack-fetch`, and a file that isn't a modpack with `archive-not-modpack`.
- **Anything else** is a modpack slug, looked up as `add --type modpack` looks one up, on the first provider holding it, or the one `--provider` names. It takes the newest `release` version that fits the project's Minecraft version and loader when importing into one, a version tagged with no loader at all fitting any loader, and any version for a new project; a specific version is the URL of its file. Importing the pack the project was last imported from never goes back past that version. A project that isn't a modpack fails with `type-mismatch`. The archive goes in the cache at its sha512, and one whose author turned third-party downloads off fails with `manual-download`, naming its page: download it into the new project's `downloads/` and run the import again.

`--type mrpack|curseforge|source|modpack` refuses anything of another kind with `usage`. `--ref` and `--path` apply to a git source and fail with `source-ref` and `source-path` on anything else, and `--provider` applies to a slug only.

A shulker source is copied: its `shulker.json`, `shulker.lock`, override folders, a feature's included, `files/`, icon, `.gitignore`, and the local files and folders its entries name. Nothing else in its folder is, such as a build an instance makes in place. The lock is brought in line as `lock` does, reusing each entry that still matches. `--name` renames the copy.

The import writes the project directory, `-C` or the current folder. Without a `shulker.json` there, the folder is created when it is missing and becomes a new project, a copy of the pack. The new project is built in a staging folder beside it and moved in only once it is whole, so an import that fails part way leaves the folder as it was. It takes every side the pack declares; `--side` keeps one, as `export --side` does: the other side's block, its entries and its override folders are left out, and `leftOut` in the result lists each by key or override path.

With a `shulker.json` there, the pack is merged into that project, and the project wins every clash. A key the project already lists, or a mod whose jar id it already has, keeps the project's entry, and an override file already at the pack's path keeps the project's file; `keptYours` lists each, and the report shows ten and counts the rest. The lock's `imported` records the archive each import came from. Importing the same pack again, the same provider project or the same archive, reads that archive back from the cache, or from its provider when the cache was pruned (a prune keeps one imported from a file), and replaces every entry and override file that still matches it: only what you changed since is kept. The entries the pack brings are written as `add` writes them, with the version the pack locked held by the lock rather than a `pin`, and the files it bundles come in as local files or overrides, as they would in a new project. Only the sides the project declares come in, narrowed further by `--side`: a client-only project leaves out the pack's server-only entries, its `server-overrides/` and its `server` block. A pack shulker exported, or a shulker source, also merges its `features`, `variables`, `providers`, `java` and `note`, and the side blocks the project declares, key by key. A pack whose Minecraft version, loader or loader version differs from the one the project sets or inherits fails with `import-mismatch` before anything is written; a project with neither takes the pack's. A merge relocks as `add` does, so an instance gets a history entry and a failure writes nothing. `--name` on a merge fails with `usage`. `--json` sets `merged` to `true`.

A Modrinth modpack is a `.mrpack`. Each mod jar the pack lists, and each resource pack, shader or datapack zip, is looked up on Modrinth by its hash and locked as that project's version. A beta or alpha version gets that `channel` in `shulker.json`, as a CurseForge file does. What Modrinth doesn't host, along with the mod jars and pack zips the pack bundles in its overrides, is then looked up on CurseForge by fingerprint, all in one request, and each exact match is locked from there. Every match is downloaded from its provider before it locks, even one the pack bundles, since every later install fetches it from there. A match whose author doesn't allow third-party downloads, or whose download fails, as when a CDN cuts the file short, stays an override, with a warning, and without a CurseForge key the lookup is skipped with a warning. A mod keeps its provider's side unless its index entry's `env` places it on a side the project builds and that side lacks, as when a pack ships a server mod on the client for singleplayer's integrated server; then it is locked on `both`, its manifest entry records `side`, and one warning and the result's `sides` list name each such mod with both sides. An `env` that matches the provider's side, or is narrower, changes nothing, since packwiz marks every mod as needed on both sides, and so does a mod with no `env`. One matched from `client-overrides/` or `server-overrides/` takes that folder's side, and its manifest entry records it too, without a warning. A matched index file whose provider download fails is fetched from the index's other URLs and kept as an override, with a warning; with none that works the import fails with `modpack-download`. A resource pack, shader or datapack is pinned to the version the pack ships and keeps the file name it ships as its `filename`, since the game enables packs by name and Paxi orders datapacks by it. A datapack is locked as one wherever the pack keeps it: in `datapacks/`, or in a global datapack mod's folder, `config/paxi/datapacks/` or `config/openloader/data/` or `packs/`, where a zip in the overrides is looked up on Modrinth too. A datapack under `resourcepacks/` is locked as a datapack, unless its zip carries `assets/`, which make it load as a resource pack there, and then it is locked as one. A copy under `resourcepacks/` or `datapacks/` with the same bytes as a zip in a global datapack mod's folder is left out as a leftover, since that mod loads the other. One under `resourcepacks/` that carries `assets/` is a hybrid's, whose assets load only from there: it locks with the loaded copy as one datapack entry with `"resourcepack": true`, or stays an override when the loaded copy does. `duplicates` lists each by its path in the archive: an index file by its index path, an override under `overrides/`, `client-overrides/` or `server-overrides/`. Anything else, a file neither provider has, goes into the project's overrides as it is. A pack shulker exported carries its own `shulker.json` and `shulker.lock` at the archive root unless its manifest turns `marker` off, and those are read in preference to the marker jar, so the project comes back as it was, resource packs and shaders included, and its `icon` is put back from the archive's `icon.png`. A local file that project had, bundled into the archive with `export mrpack --bundle`, becomes the new project's own local file in `files/`, so it builds on a machine that never read the archive; two of one name fail with `file-taken`. `--ignore-shulker` skips both and imports the archive as any other Modrinth modpack.

When the pack places a seed mod, Config Manager, YOSBR or Configured Defaults, each override under its folder (`config/modpack_defaults/`, `config/yosbr/` or `configureddefaults/`) moves to its own path in the same override folder and that path joins `seedFiles`, so the build seeds it and an export puts it back in the mod's folder. The mod stays in `requires`. Where the pack also ships the plain path, the plain copy stays an ordinary override and the default is dropped with a warning, since a launcher extracts the plain one first. `import` reports the count, `3 seeded files from config/modpack_defaults`, and the result's `seeded` lists the paths by folder. A merge into an existing project adds to its `seedFiles`.

A CurseForge modpack is a `.zip`, the kind the CurseForge app exports and [`export curseforge`](#shulker-export-curseforge) writes. Each mod, resource pack and shader the pack names is locked by its CurseForge project and file ID, so the project gets the exact files the pack ships, and the pack's overrides folder becomes the project's `overrides/`, less the marker jar of the shulker project that exported it, since the project builds its own. A file the pack marks optional is skipped with a warning. A beta or alpha file gets that `channel` in `shulker.json`, so a later `lock` keeps it and `update` moves within it. The pack's `recommendedRam` becomes the project's `client.memory`, in whole gigabytes when it is a multiple of 1024 MB and in megabytes otherwise. A file whose author doesn't allow third-party downloads is looked for on the manifest's other providers by its sha1 first, and one Modrinth hosts with the same bytes locks as that Modrinth project, with a warning naming the provider it was taken from. One found nowhere is named with its page, and the new project's `downloads/` is created for it. At a terminal the import then waits on a checklist of the files: it reads the folder, and the folders [`downloads.watch`](#configuration) names, every 5 seconds, or at once on Enter, matching each file by hash whatever its name, ticks off each file it finds, and goes on by itself once every one is there. A file dragged into the terminal, or its path pasted, is taken the same way. Esc skips the files still missing: the import finishes, and each skipped mod stays in the lock pending, with its CurseForge sha1 and no `sha512`, for `install` to ask for again. Ctrl-C stops the import. Off a terminal, or with `--no-input` or `--json`, it fails `missing-files` instead, naming each file: download them into `downloads/` and run the import again. Either way they lock as manual downloads.

When CurseForge pairs server files with the pack's version, `import` reads their file list by ranged requests, without downloading them, and they decide each mod's side. A mod they leave out gets `"side": "client"` in `shulker.json`, and one they ship that its own metadata made client-only gets `"side": "both"`, so the server builds what the pack's author ships; a mod whose side the marker already set keeps it. The report counts both. The pack is found by its hash, or failing that by searching CurseForge's modpacks for its name and matching the file's sha1. Server files with no `mods` folder, or that can't be read, change nothing, with a warning. `--no-server-pack` skips the step.

A zip shulker exported carries its own `shulker.json` and `shulker.lock` at its root unless its manifest turns `marker` off, and those are read as a Modrinth pack's are, so the project comes back as it was: features, side blocks, conditions and keys chosen with `--as` included. Each file the zip names is matched to that project's lock by its bytes once it is downloaded, and comes back as the project locked it, so a mod it locked from Modrinth is locked from Modrinth again. A local file the export bundled becomes the new project's own local file in `files/`. A feature's override files ship in `overrides/` with the rest, and `shulker.overrides.json` beside the manifest records the folder each came from, so they go back into the feature's folder. `--ignore-shulker` skips both and imports the zip as any other CurseForge modpack.

```sh
shulker import ~/Downloads/fabulously-optimized.mrpack
shulker import ~/Downloads/all-the-mods.zip -C all-the-mods
shulker import pack.mrpack -C my-pack --name my-pack --side client
shulker import fabulously-optimized
shulker import base-pack --type modpack
shulker import https://github.com/friends/pack.git --ref v2 --path packs/survival
```

| Flag | Description |
| --- | --- |
| `--name <name>` | Project name (default: the modpack name, slugified, or the name a pack with a shulker marker was exported under) |
| `--type <kind>` | Refuse the modpack unless it is this kind: `mrpack`, `curseforge`, `source`, or `modpack` for one the project requires (default: detected) |
| `--provider <name>` | Look a slug up on this provider only: `modrinth` or `curseforge` (default: the first that has it) |
| `--ref <ref>` | Git ref of a git source (default: the remote HEAD) |
| `--path <folder>` | Folder of a git source's repository holding its `shulker.json` (default: the root) |
| `--side <side>` | Take one side only: `client` or `server` (default: every side the pack declares) |
| `--ignore-shulker` | Ignore the shulker manifest and lock inside the modpack and import it as any other one |
| `--no-server-pack` | Don't read the server files the pack pairs with for which mods are client-only |
| `-v, --verbose` | Print a line for every file fetched, rather than one count per group |

### `shulker export mrpack`

Export the project as a Modrinth modpack for the Modrinth app and other launchers. Resource packs and shaders go in alongside the mods, as client-only files. A datapack goes in once for each folder the exported sides place it in, for the sides that place it there, so a server's copy in its world and a client's in `datapacks/` are two files. The archive carries the project's own `shulker.json` and `shulker.lock` at its root, with `shulker.overrides.json` naming the feature folder each of a feature's override files came from, so [`import`](#shulker-import) restores the project it came from. The source is the project in the current directory, or a project directory, git URL, or manifest URL; a git or URL source is downloaded first and the archive is written to the current directory. Locked files the exported sides use and the cache lacks are downloaded first, so a fresh checkout exports without `shulker install`. With a warm cache the export stays offline. A local `file` entry has no download link, so it needs `--bundle` and goes inside the archive, labelled `local file`.

A launcher writes every override over the player's copy on each update, so a seeded file, one the manifest's `seedFiles` lists, can only stay seeded through a mod in the game. When the exported side places a seed mod, each seeded file goes under that mod's folder instead of its own path, and the mod copies it into place only where the player has none: Config Manager's `config/modpack_defaults/`, YOSBR's `config/yosbr/` or Configured Defaults' `configureddefaults/`, the first of those the side places. A file merged key by key goes in as the build renders it. With none placed, seeded files ship as plain overrides and the export warns once, listing them and naming `configmanager`, `yosbr` or `configured-defaults` to add. A build never moves them, since shulker seeds them itself.

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
| `--fail-fast` | Stop at the first file that fails to download, rather than trying them all |
| `--bundle` | Put files that Modrinth launchers can't download inside the archive, each in the override folder for its side: `overrides/` for both, `client-overrides/` or `server-overrides/` for one side |
| `--assume-client` | Export a client even when the source declares none, from the mods and overrides both sides share |
| `--ref <ref>` | Branch, tag, or commit to export from a git source (default: the remote HEAD) |
| `--path <path>` | Folder of a git source's repository that holds its shulker.json (default: the root) |

### `shulker export curseforge`

Export the client side as a CurseForge profile `.zip` for the CurseForge app's Import Profile. Mods, resource packs and shaders locked from CurseForge go in by file ID. Everything else is looked up on CurseForge by its fingerprint, and matches go in by file ID too. When a Modrinth file's fingerprint misses, the export finds the CurseForge project by the lock's CurseForge alias or the Modrinth slug, downloads the file with the same name and size for the locked Minecraft version and loader, and uses it only if every zip entry unpacks to the same bytes, warning that the bytes differ. This catches the same build uploaded to both sites, where only the entry timestamps differ. Files that aren't on CurseForge fail the export unless `--bundle` ships them inside the archive, which the CurseForge app warns about on import. A datapack goes in by file ID only when the client places it in `datapacks/`, where the CurseForge app installs datapacks; one placed in a global datapack mod's folder fails the export with `curseforge-cant-place` unless `--bundle` ships it there. A resource pack or shader that goes in by file ID is enabled in `options.txt` and the shader mod's config under CurseForge's own file name, since that is what the launcher saves it as. Server-only mods and files are left out. The profile gets shulker's logo as its image, and the archive carries `shulker.json` and `shulker.lock` at its root. The source works as in [`export mrpack`](#shulker-export-mrpack).

A launcher writes every override over the player's copy on each update, so a seeded file, one the manifest's `seedFiles` lists, can only stay seeded through a mod in the game. When the exported side places a seed mod, each seeded file goes under that mod's folder instead of its own path, and the mod copies it into place only where the player has none: Config Manager's `config/modpack_defaults/`, YOSBR's `config/yosbr/` or Configured Defaults' `configureddefaults/`, the first of those the side places. A file merged key by key goes in as the build renders it. With none placed, seeded files ship as plain overrides and the export warns once, listing them and naming `configmanager`, `yosbr` or `configured-defaults` to add. A build never moves them, since shulker seeds them itself.

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
| `--fail-fast` | Stop at the first file that fails to download, rather than trying them all |
| `--bundle` | Put mods that aren't on CurseForge inside the archive, and bundle every mod not from CurseForge when the lookup can't run |
| `--assume-client` | Export a client even when the source declares none, from the mods and overrides both sides share |
| `--ref <ref>` | Branch, tag, or commit to export from a git source (default: the remote HEAD) |
| `--path <path>` | Folder of a git source's repository that holds its shulker.json (default: the root) |

## Mods

### `shulker add`

Add mods to the manifest, resolve them and their dependencies, and write the lock. Mods are named by their provider slug or project id, by a Modrinth or CurseForge URL, or by a path to a local jar or zip, which becomes a local `file` entry. CurseForge's search leaves some projects out, so a slug it misses fails `mod-not-found` with help pointing at a file URL or the project id, which its page shows under About Project. A CurseForge file whose author doesn't allow third-party downloads is looked for on the manifest's other providers by its sha1, and one Modrinth hosts with the same bytes locks as that Modrinth project instead, with a warning naming the provider it was taken from; otherwise it fails `manual-download`. At a terminal `add` first waits for the file on the same checklist as [`install`](#shulker-install), and adds it once it is in `downloads/` or a [`downloads.watch`](#configuration) folder; Esc ends the wait and the add fails `manual-download` all the same. With `--type modpack` the argument is a modpack instead: a local path, git URL, raw manifest URL, archive, or the slug of a Modrinth or CurseForge modpack. A bare `add` of a slug the provider files as a modpack adds it as one too. Each type takes only the flags that mean something for it, so `--ref` on a mod or `--side` on a modpack is refused.

With no arguments, `shulker add` asks `Add which mods?` over the search box [`shulker search`](#shulker-search) opens. Tab or enter moves into the results, space marks one, and shift+tab goes back to refine the query without losing the marks. Enter adds everything marked, or the row under the cursor when nothing is, in one change to the lock, each from the provider it was found on. `--type`, `--provider` and the other flags still apply, and `shulker resourcepack add` and `shulker shader add` search their own type. Off a terminal, or with `--no-input` or `--json`, the argument is required, and so is a modpack's.

```sh
shulker add sodium lithium
shulker add iris --channel beta
shulker add betterthirdperson --provider curseforge --side client
shulker add sodium --as speed
shulker add ../base-pack --type modpack --as base
shulker add cobblemon-official --type modpack
shulker add https://modrinth.com/mod/sodium https://www.curseforge.com/minecraft/mc-mods/jei/files/5000001
shulker add
```

A URL names the provider and the project, and a URL that names a file or version pins it, as `--pin` would; several URLs in one `add` each keep their own pin, in one change to the lock. `--provider`, `--pin` or `--type` that disagrees with the URL is a `usage` error. The type comes from the project, not the URL's section. Accepted shapes, with query strings and fragments ignored:

- `https://modrinth.com/<mod|project|plugin|resourcepack|shader|datapack|modpack>/<slug or id>`, optionally followed by `/version/<id or number>`
- `https://cdn.modrinth.com/data/<project>/versions/<version>/<file>`
- `https://www.curseforge.com/minecraft/<mc-mods|texture-packs|shaders|data-packs|modpacks>/<slug>`, optionally followed by `/files/<file id>` or `/download/<file id>`; also on `curseforge.com` and `legacy.curseforge.com`
- `https://www.curseforge.com/projects/<project id>`

A CurseForge file URL is resolved by its file id, so it reaches a project the slug search misses. Any other URL on these hosts is a `usage` error listing the shapes, and a URL on another host keeps its meaning as a modpack source.

| Flag | Description |
| --- | --- |
| `--type <type>` | What the arguments name: `mod` (default), `modpack`, `resourcepack`, `shader`, `datapack` |
| `--side <side>` | Override side: `client`, `server`, `both` |
| `--resourcepack` | Datapacks only: also place the zip in `resourcepacks/`, for one that carries `assets/`. Implies `--type datapack` |
| `--channel <channel>` | Least stable channel accepted: `release`, `beta`, `alpha` |
| `--pin <version-id>` | Pin to a provider version id (one mod or modpack only; a URL argument carries its own pin). A beta or alpha file widens the entry's `channel` to match, as [`shulker pin`](#shulker-pin) does |
| `--provider <provider>` | Provider to use for this mod or modpack: `modrinth` or `curseforge` |
| `--ref <ref>` | Branch, tag, or commit for a modpack's git source |
| `--path <path>` | Folder of a modpack's git repository that holds its shulker.json (default: the root) |
| `--as <key>` | Key used in `requires`, messages, and `requiredBy` (default: a mod's jar id, a pack or hosted modpack's provider slug, a modpack archive file's name, the name in a modpack's manifest) |
| `--unlocked` | Resolve a modpack's mods here instead of copying the versions its lock pins |
| `--no-auto-update` | Keep a modpack at its locked version on `shulker sync`; `shulker update` still moves it |
| `--with-deps` | Move dependency versions the lock holds when a mod being added needs another. One a locked modpack pins is listed in `shulker.json` as it moves, so it no longer follows the modpack. On a terminal, an add without it prints what would have to move and asks `Move it?` (`Move them?` for several), and yes does the same |
| `--skip-missing` | Add what resolves and skip each name that isn't found or has no compatible version, with a warning for each. Without it, `add` looks every name up first and, when any misses, adds nothing: the error lists each name that missed and gives the command that adds the rest. A provider it can't reach still fails the whole command, since the name may be there |
| `-y, --yes` | Answer Yes to what `add` asks: moving a version the lock holds, as `--with-deps` does, and unlocking a modpack built for another Minecraft |
| `-v, --verbose` | Print a line for every file fetched, rather than one count per group |

An argument that names an existing file, or ends in `.jar`, `.zip` or `.mrpack`, is a local file rather than a slug, and is locked in the same run. A file inside the project is referenced where it lies. One outside it is copied into `files/`, and so is one in `downloads/`, an overrides folder or a folder a side builds into, since those files aren't the project's to keep. Adding the same file again refreshes its copy and relocks it, which is how a rebuilt jar gets in; a different file already in `files/` under the same name is never replaced. The key is a jar's mod id or, for a pack, its file name without the extension, lowercased with anything a key can't hold turned into dashes, and `--as` overrides either. A jar is a mod, and a bare `add` reads a zip's type from what it holds: a resource pack holds `pack.mcmeta`, a datapack `pack.mcmeta` and `data/` without `assets/`, a shader `shaders/`. One with both `data/` and `assets/` needs `--type`. `--pin`, `--channel` and `--provider` don't apply to a local file.

A folder is taken the same way, as a resource pack, shader or datapack built from its sources, which `lock` zips. One inside the project is referenced where it lies; one outside it, or in one of those folders, is copied whole into `files/<name>/`, leaving out what the zip leaves out, and `add` says so. Adding it again replaces the copy. Its key is the folder's name, made a key the same way, and a bare `add` reads its type from its root the way it reads a zip's: `pack.mcmeta` for a resource pack or datapack, a `shaders/` folder for a shader. A folder can't be a mod, and one the project lies in can't be copied into it. A folder the game or Iris wouldn't load is locked all the same, with a warning at `add` and `lock` naming what is missing: a resource pack needs `pack.mcmeta` at its root, valid JSON with `pack.description` and a format (`min_format` and `max_format`, or `pack_format`), and a shader needs `shaders/`. The format isn't checked against the project's Minecraft version.

```sh
shulker add ./build/libs/my-mod-1.0.jar
shulker resourcepack add ~/Downloads/Faithful.zip
shulker resourcepack add "./Resource Packs/Mod Menu Helper"
```

### `shulker search`

Search every provider shulker has set up for projects matching the words, and print them as one table, most downloaded first, with the slug to add each by. A Modrinth and a CurseForge result are one row when their slug and type match and so do their names, once tags like `(Fabric)` or ` - DISCONTINUED` and punctuation are dropped, or their authors; the current project's lock pairs any others it holds as one mod, and so does the listing index in the cache, which remembers each pair `add`, `lock` or `import` proved by a jar's mod id or a file's hash. Source says which provider a row was found on, or `both`, and goes under `--provider`; Downloads is the sum. The command writes nothing to the project: `shulker.json` and the lock only change through `add`. Projects CurseForge classes as something shulker has no entry type for, worlds and plugins among them, are left out. A `modpack` row is a provider modpack, which `add` takes by its slug.

With no words, `shulker search` opens a search box over a table of results that follows it as you type, searching once you pause for a quarter of a second and have typed at least two characters. Tab or enter moves into the table, which the arrow keys and the mouse wheel scroll, and shift+tab moves back to the box. Enter on a result opens its details: its author and summary, its id, downloads and page on each provider, and, in a project, whether it has a version for the project's Minecraft and loader. There, `o` opens its page in the browser, `a` adds it as `shulker add <slug>` would and leaves (in a project only), and esc goes back to the results. Esc or ctrl-c leaves and prints nothing. A query that fails keeps the last results on screen, with the error under the box. Off a terminal, or with `--no-input` or `--json`, the words are required.

Search runs in the current directory's project when there is one, and without one when there isn't. A project named with `-C` or `-i` has to open, so a folder with no `shulker.json` or an instance that isn't registered is an error.

```sh
shulker search
shulker search sodium
shulker search fresh animations --type resourcepack
shulker search jei --provider curseforge --limit 5
```

| Flag | Description |
| --- | --- |
| `--type <type>` | Only projects of one type: `mod`, `modpack`, `resourcepack`, `shader`, `datapack` |
| `--provider <provider>` | Search one provider instead of every available one: `modrinth` or `curseforge` |
| `--limit <n>` | Results to print per provider (default 10, as many as each provider answers with: at most 100 from Modrinth, 50 from CurseForge) |
| `-v, --verbose` | Also print each provider's id, and its downloads in place of the sum |

With `--json`, `data.results` lists each row as `{ "slug", "title", "type", "side", "author", "summary", "downloads", "providers" }`, where `providers` holds its hit on each provider as `{ "provider", "id", "title", "downloads", "page" }`, and `data.query` is the words as one string.

### `shulker remove`

Remove mods from the manifest and prune dependencies nothing else needs. A key that names a modpack removes the modpack and the mods only it provided. Alias: `rm`.

```sh
shulker remove lithium
shulker remove base --type modpack
```

| Flag | Description |
| --- | --- |
| `--type <type>` | What the arguments name: `mod` (default), `modpack`, `resourcepack`, `shader`, `datapack` |

### `shulker list`

List every `requires` entry under a heading per type, with its locked version and where it comes from. Mods a modpack or another mod pulled in are listed too, with `from <modpack>` or `required by <mods>`. A local `file` entry shows its path in place of a version, labelled `local file`, and its JSON entry carries `file` with no `version` or `provider`. Alias: `ls`.

```sh
shulker list
shulker list --type modpack
```

| Flag | Description |
| --- | --- |
| `--type <type>` | Only entries of one type: `mod`, `modpack`, `resourcepack`, `shader`, `datapack` |

### `shulker lock`

Bring `shulker.lock` in line with `shulker.json` after you edit it by hand, without upgrading anything. Mods new to `shulker.json` are resolved, mods nothing lists or requires are dropped, and a mod whose channel, pin, provider, or project changed is picked again. A new `side` is taken as it is, at the locked version, without asking the provider. Every other mod keeps its locked version, and modpacks stay at their locked commit unless their `ref` changed. When the locked Minecraft or loader version no longer matches `shulker.json`, or a mod is locked from a provider `shulker.json` no longer lists, every mod is resolved again and `reresolved` says why. Without a `shulker.lock`, `lock` creates one. It is the one command that tolerates a lock it can't read, one that fails `lock-invalid` or `schema-newer` everywhere else: it renames that file to `shulker.lock.replaced`, warns naming why it couldn't be read, and writes a fresh lock. Only the latest replaced lock is kept.

`add`, `remove`, `update`, `pin`, `unpin`, `modpack add`, and `modpack remove` do the same before their own change, so a hand edit is never left out of the lock. What they bring in shows up in their output.

With keys, `lock` also looks each named entry up again from its provider at the version it is locked at, and rewrites the file the lock names for it: its URL, hashes, size and filename. Its dependencies, side and channel stay as locked, and every other entry is left alone. This is how a lock whose file doesn't match its provider is put right without moving it to a newer version the way `update` does. A hosted modpack is locked again at its version, as `pin` does, with its pin in `shulker.json` left as it was: its archive is fetched again and the mods it brings are laid again. A version the provider no longer has fails with `version-not-found`; run `update <key>` to move to one it still has. A local file, or a modpack from a git, URL or folder source, has no provider version to look up and fails with `local-file` or `not-on-provider`. An entry a modpack brings fails with `modpack-provided`, since the modpack would lay it again.

```sh
shulker lock
shulker lock sodium
```

### `shulker check`

Check the project the way CI should, on every push: fail when anything would break it, and change nothing. It writes nothing to the project: no `build/`, no lock, no history entry. Every problem is reported, not just the first, and the exit code is non-zero when there is any.

Name the checks to run; with none, `check` runs `lock`, `files` and `deps`, which is everything an export needs. `--all` runs every check, `server` included when the project declares a server.

- `lock`: `shulker.lock` must match `shulker.json`. Each difference is listed under `lock-stale`, and `check` never relocks. It downloads nothing.
- `files`: every locked file must be obtainable. Each is put in the download cache the way `install` does, so a run with the cache kept from the last one downloads nothing. Every file is tried, so one failed download doesn't hide the next, unless `--fail-fast`. The failed downloads are one problem, `download-failed`, and the manual downloads missing from `downloads/` and local files gone with no copy in the cache another, `missing-files`, as `install` reports them.
- `deps`: every side the manifest declares is validated as `install` validates it, with every mod jar read: a mod whose dependency is missing or the wrong version fails with the same line and `shulker ignore` hint, and a problem only the server has fails a project that declares both sides. `ignore` entries and loader dependency overrides apply as usual. Without `files`, it fetches the mod jars alone.
- `server`: the server jar and, unless `shulker.json` sets `java`, the Java runtime the server runs on must download, as `install` fetches them. A server jar the lock doesn't record yet is fetched but not locked. Naming it on a project that declares no server fails `no-side`.

Warnings, such as an `ignore` entry that matches nothing or a local file served from the cache because the project's copy is gone, print without failing the run, unless `--strict` is passed.

```sh
shulker check
shulker check lock
shulker check --all
```

| Flag | Description |
| --- | --- |
| `--all` | Run every check, `server` included when the project declares a server; not with named checks |
| `--strict` | Fail on warnings too |
| `--fail-fast` | Stop at the first file that fails to download, rather than trying them all |

In a GitHub Actions run each problem item is also an annotation on the run (see [`--annotations`](#global-flags)); `check-failed` adds none of its own, since it repeats them.

With `--json`, `data.scopes` lists the checks that ran and a clean run's `data.problems` is empty. A failing run ends with `check-failed`: its `items` are every problem's own items, each as `<code>: <item>`, one per thing to annotate, and `data.problems` lists each problem as an error, `{ "code", "message", "items", "help" }`.

### `shulker audit`

Report what in the project, or an instance with `-i`, deserves a closer look. `check` is about whether a project builds; `audit` is about where its files come from. It reads the lock and the files on disk, and goes online only for the takedown check.

- **Takedowns:** a locked file its provider no longer has, asked in one request per provider: Modrinth by sha512, CurseForge by the fingerprint of the copy in the cache. Neither says why a file went, and authors delete their own old versions too, so a vanished file is a reason to look, not proof. `shulker update <key>` moves off it and `shulker remove <key>` drops it. A provider that can't be asked, because shulker can't reach it or CurseForge has no key, is reported as skipped rather than passed, and a CurseForge file the cache doesn't hold, or a CurseForge modpack, which its fingerprints leave out, isn't checked.
- **Provenance:** a lock entry that names a provider but downloads from outside that provider's hosts, the entry every other command refuses as `provenance-mismatch`, or a file its provider files under another project than the lock names. `shulker lock <key>` looks it up again.
- **Unpublished files:** every jar and pack no provider published, with where it comes from: a local `file` entry, an entry downloaded from a URL of its own, or a file an override folder lays, a modpack's included and every feature's folder with them.
- **Installed jars:** every jar in `mods/` whose bytes no longer match what the lock names, or what the build recorded for one an override laid, and every jar there that neither accounts for. A project's are its sides' build directories; an instance's is its own directory. `shulker build --force` puts the locked copies back.
- **Young versions:** locked versions published more recently than `security.minReleaseAge`.

Name keys to audit only those entries, and every entry a named modpack brings; a key the lock doesn't hold fails `mod-not-found`. Files that no entry names, such as override jars and unlisted jars in `mods/`, are left out of a narrowed audit. A key that is also a subcommand of `audit` goes after `--`: `shulker audit -- jar`.

It exits non-zero for takedowns and provenance problems, ending with `audit-failed`, so a pack author's CI can gate on it. Unpublished files, installed jars and young versions are listed but don't fail it, since a pack may reasonably have them.

With `-i`, an instance that builds in place is audited as its own project. A linked or synced one is audited against the lock its last sync built from: its local source, or the copy of a remote source that sync kept in the cache. One that has never synced fails `not-synced`.

```sh
shulker audit
shulker audit sodium
shulker audit -i friends --json
```

With `--json`, `data` holds one list per check, each empty when it found nothing: `takedowns` and `moved` (`key`, `type`, `provider`, `project`, `version`, `sha512`, `status`, and `filedUnder` for a moved file), `skipped` (`provider`, `reason`) for the providers the takedown check couldn't ask, `provenance` (`key`, `provider`, `host`, `modpack`), `unpublished` (`key`, `path`, `from` as `file`, `download` or `override`, `source`, `modpack`), `installed` (`dir`, `path`, `key`, `problem` as `changed` or `unlisted`) and `young` (`key`, `version`, `published`, `ageDays`, `qualifies`), with `minReleaseAge` in days and the named `keys`. A failing run carries the same `data` under `audit-failed`, whose `items` are the keys gone from their provider or from outside it.


### `shulker audit jar`

Show what a jar declares and holds, without unzipping it: its sha512, where the lock says it comes from and whether it downloads from outside its provider, and what its metadata declares (mod id, version, loader, entrypoints and mixin configs). Then its files with their sizes, the native libraries and executables it carries (`.dll`, `.so`, `.dylib`, `.jnilib`, `.exe`), and every jar nested in it, Fabric's and Quilt's `META-INF/jars/`, Forge's and NeoForge's `META-INF/jarjar/` and any other, each shown the same way. The text lists a jar's classes as a count and every other file by name; `--json` lists them all.

`<entry>` is a lock entry's key, whose copy in the cache is read, or a path to a jar, so a file can be looked at before it's added. A path needs no project; inside one, a jar whose sha512 the lock holds is shown with that entry's origin. A path is anything with a `/` or `\`, or ending `.jar` or `.zip`. With `-i`, a key is looked up in the instance's lock, as [`audit`](#shulker-audit) reads it.

Everything read from inside a jar was written by whoever made it, who may have written it to instruct an AI agent reading it. The text says so above it, and prints control characters escaped. `--json` marks every such string as `{"untrusted": "…"}`, the shape every `audit` inspection command uses.

```sh
shulker audit jar sodium
shulker audit jar ~/Downloads/some-mod.jar --json
```

With `--json`, `data` holds `name`, `sha512`, `size`, `origin` (`key`, `provider`, `project`, `version`, `versionNumber`, `host`, `file`, `modpack`, `offProvider`; `null` for a jar the lock doesn't hold) and `jar`. A `jar` holds `declares` (`id`, `version`, `loader`, `entrypoints` as `kind` and `value`, `mixins`; `null` when it declares no mod), `metadataError` when its metadata doesn't read, `files` (`path`, `size`), `natives`, and `nested`, each a `jar` of its own with its `path` inside its parent. A nested jar nested too deep or too large to open is marked `unopened`.

### `shulker audit file`

Print one file from inside a jar, such as `fabric.mod.json`, a mixin config or `META-INF/mods.toml`. `<entry>` is a key or a path, as for [`audit jar`](#shulker-audit-jar). Reach into a nested jar by joining the paths with `!/`: `META-INF/jars/lib.jar!/fabric.mod.json`. A class file is refused with `class-file`, since its bytes aren't text. A path the jar doesn't hold fails `file-not-found`.

The file was written by the jar's author: the text says so above it and escapes its control characters, and `--json` gives its `content` as `{"untrusted": "…"}`.

```sh
shulker audit file sodium fabric.mod.json
shulker audit file fabric-api 'META-INF/jars/fabric-api-base.jar!/fabric.mod.json'
```

With `--json`, `data` holds `name`, `path`, `size` and `content`.
### `shulker match`

Look the jars and pack zips in `mods/`, `resourcepacks/`, `shaderpacks/` and the datapack folders (`datapacks/`, `config/paxi/datapacks/`, `config/openloader/data/`, `config/openloader/packs/`) of `overrides/`, `client-overrides/` and `server-overrides/` up on Modrinth by sha1, then the ones Modrinth lacks on CurseForge by fingerprint, the way `import` does, and lock each match: it joins `requires` with the side of the folder it was in, and its file leaves the override folder. Naming paths looks up only those files. A file stays an override when no provider has it, when its author doesn't allow third-party downloads, when the provider's download fails, or when `requires` already has its key, with a warning for each but the first. A zip in a datapack folder locks as a `datapack`. Feature override folders are left alone. `locked` lists the keys, `moved` the files they came from, and `kept` the files left as overrides. Every match is locked without asking; a lookup needs the network, and without a CurseForge API key only Modrinth is asked.

```sh
shulker match
shulker match overrides/mods/sodium-fabric-0.6.13+mc1.21.1.jar
shulker match --dry-run
```

| Flag | Description |
| --- | --- |
| `--dry-run` | Look the files up and report what would be locked, changing nothing |

### `shulker update`

Re-resolve mods to the newest compatible versions. With no arguments, fetches every modpack again, whatever its `autoUpdate`, and updates every mod; naming a modpack updates it and its mods. Local `file` entries have no newer version to move to: a bare `update` leaves them as they are, and naming one says it is a local file. A version published more recently than [`security.minReleaseAge`](#configuration) is held back for the newest old enough, never older than the version already locked, and `update` warns with each one held back, how old it is, the day it qualifies and the `pin` that takes it now; under `--json` they are in `securityWarnings`, as `data.held` with `key`, `took`, `skipped`, `skippedId`, `published`, `ageDays` and `qualifies`. `add` and `lock` hold back and warn the same way. In an instance (a project whose side builds into its own directory), `update` then builds that side in place, backing up its worlds first when the mods change, and leaves the instances synced from it to `shulker sync`, which it names when there are any; elsewhere it only writes the lock and `shulker install` builds it. Alias: `upgrade`.

```sh
shulker update
shulker update sodium iris
```

| Flag | Description |
| --- | --- |
| `-v, --verbose` | Print a line for every file fetched, rather than one count per group |

### `shulker outdated`

Show mods, and modpacks from a provider, with a newer compatible version without changing anything, like a dry run of `update`. A modpack's row is marked `modpack`. A newer version [`security.minReleaseAge`](#configuration) holds back is marked `held back` with its age and the day it qualifies, with the `pin` that takes it now; under `--json`, `held` holds its facts. Local `file` entries are skipped, and naming one says it is a local file rather than that it is up to date.

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

A mod can name another by its Modrinth or CurseForge slug, which the loader never matches. When that slug, or a key in `requires`, belongs to a locked mod, `suggests` marks the line `installed as <key>` and `add` and `update` leave it out.

With `--json`, `data.suggestions` lists each one as `{ "mod", "kind", "on", "declared" }`, where `kind` is `recommends`, `suggests`, or `optional`, plus `installedAs` for such a match.

### `shulker pin`

Pin a mod, or a modpack from a provider, to a provider version id, or to the version a Modrinth or CurseForge URL names. The URL must be of the provider and project the mod is locked from, else `usage`; to switch projects, `remove` it and `add` the URL. Without a version, pins it to the version already in the lock. Pinning a mod or modpack to a beta or alpha file accepts that channel for it: its `channel` in `shulker.json` widens to match, with a warning, so its dependencies may be that channel too and it keeps following it after `unpin`. A local `file` entry has no provider version, so `pin` and `unpin` refuse it with `local-file`. A pin takes its version whatever [`security.minReleaseAge`](#configuration) says, with a warning naming its age while it is younger.

```sh
shulker pin iris k9RhZq2X
shulker pin jei https://www.curseforge.com/minecraft/mc-mods/jei/files/5000001
shulker pin sodium
```

### `shulker unpin`

Remove a mod's or a hosted modpack's pin and re-resolve it.

```sh
shulker unpin iris
```

### `shulker ignore`

Record that a dependency problem between a mod and what its jar declares about another is safe to ignore. A problem reported by `add`, `remove`, `update`, `lock` or `install` prints the exact command to run, with the rule and the range the jar declares. The entry lands in `ignore` in `shulker.json` and the problem stops failing validation for as long as the jar declares that range; a new version that declares a different range makes the entry stale and the problem comes back. Without `--declared`, the command reads the rule and range from a matching problem in the locked mods. An existing entry for the pair is only replaced with `--force`.

On Fabric, validation first applies `config/fabric_loader_dependencies.json`, Fabric Loader's own dependency overrides, as the build lays it from the override folders. A dependency the file removes is no problem and needs no ignore, and one it adds is checked like any other, each side by its own build's file. Quilt doesn't read the file.

Validation checks each side the project declares, or the client and the server when it declares none, against the mods that side's build places, so a client mod that needs a `server` mod fails, since the client build leaves that mod out. A problem only some sides have names them, and one whose dependency is locked but placed only on the other side suggests `shulker set requires.<key>.side both`.

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

`set`, `unset`, and `get` edit and read any field of `shulker.json` by its dotted path, like `server.memory` or `requires.sodium.channel`. They never change `shulker.lock`. When an edit leaves the lock out of date, they warn and name each difference; `shulker lock` brings it back in line.

Inside a map of plain values (`server.properties`, `variables`, `client.options`, `links`), everything after the map's name is the key, so `server.properties.rcon.port` needs no escaping.

With `--json`, `set` and `unset` return `{ "path", "from", "to" }`, leaving out `from` when the field wasn't set and `to` after `unset`. `get` returns the value itself.

### `shulker set`

Set a field. A plain value becomes the most specific type the field allows: `true` and `false` are booleans and `25565` is a number where the field takes one; anything else is a string. Lists, objects, and a value that must stay a string take JSON with `--literal`. For `server.players.whitelist`, `ops`, and `bans`, a player name, uuid, or `name:uuid` adds that player to the list. A player already listed is left alone, except that `name:uuid` fills in whichever half the entry lacks; a half that contradicts the entry is an error.

The edited `shulker.json` is checked against the schema before anything is written, and the error names the field.

```sh
shulker set server.memory 6G
shulker set server.properties.max-players 20
shulker set variables.zip --literal '"02134"'
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

`--locked` reads `shulker.lock` instead, by the lock's own field names, so `minecraft` is the exact version rather than the range `shulker.json` may hold. With no lock it fails with `lock-not-found`.

```sh
shulker get name
shulker get server.properties
shulker get --locked minecraft
shulker get --locked loader.type
```

| Flag | Description |
| --- | --- |
| `--locked` | Read `shulker.lock` instead of `shulker.json` |

## Configuration

`config get`, `config set`, and `config unset` read and change shulker's own `config.json`, which applies to every project. It lives in your user config directory, or wherever `SHULKER_CONFIG` points. `instances` and `saves` name directories under shulker's data directory — `~/Library/Application Support/shulker` on macOS, `%AppData%\shulker` on Windows, `~/.local/share/shulker` on Linux, or wherever `SHULKER_DATA` points — while `store` defaults into the cache and `registry` beside `config.json`. Each of the four path keys takes an absolute path, or one relative to the directory holding `config.json`:

| Key | Description |
| --- | --- |
| `accounts.stores` | Where accounts are read from, in order, as a JSON array of `shulker`, `prism`, `multimc`, `mojang`, `atlauncher` or `gdlauncher`. Without it, `["shulker"]`. An account in several stores is counted once, and the earliest one wins |
| `accounts.default` | The id of the account a launch uses when nothing else names one. [`shulker accounts use`](#shulker-accounts-use) sets it |
| `play.memory` | The heap size [`shulker play`](#shulker-play) gives the game, like `6G`. An instance's own `memory` wins over it, as each `play.` key's instance setting does; see [`shulker instance`](#shulker-instance). Without either, the pack's `client.memory`, and without that `4G` |
| `play.jvmArgs` | Extra JVM arguments for those launches, as a JSON array |
| `play.java` | The java those launches run: the absolute path of a java binary, or of a Java home. Without it, shulker's managed runtime |
| `play.window` | The window size those launches open at, like `1280x720` |
| `play.wrapper` | A command those launches run through, as a JSON array like `["gamemoderun"]` |
| `play.saveBackups` | How many automatic backups of a save group or instance's worlds to keep, taken before `update` or `sync` changes the mods. After each one, the oldest `before update` and `before sync` backups past this number are deleted; one taken by `shulker backup` or before a restore never counts and is never deleted. An instance with no worlds backs up as nothing. A backup that can't be written stops the `update` or `sync` before any mod changes; one whose worlds can't be found, because a file it reads is unreadable, is a warning, and the change goes ahead. Without it, 5; `0` takes none. No instance setting overrides it |
| `log.keepDays` | How many days of runs `log.jsonl`, beside `config.json`, keeps. Each run that writes to it drops the entries older than this, and so does `shulker log` before it reads. Without it, 30; the least is 1. Past 4 MiB the oldest entries go whatever their age, down to 3 MiB |
| `security.minReleaseAge` | How many days old a Modrinth or CurseForge version must be before shulker chooses it, when `add`, `update` or `lock` picks a version or `sync` floats a hosted modpack. A newer one is held back, and the command says which, for how long, and how to pin it now; one with nothing old enough fails `release-too-new`. A pin takes its version whatever its age, with a warning, and a source's lock installs as its author locked it, with a warning for each entry younger than this. Without it, 7; `0` turns the check off |
| `curseforge.key` | Your CurseForge API key. `SHULKER_CURSEFORGE_KEY` takes priority when it is set |
| `downloads.watch` | Folders a wait for manual downloads also looks in, as a JSON array; a leading `~` is your home folder. Without it, your Downloads folder: `~/Downloads`, or on Linux the one `xdg-user-dirs` names; `[]` looks only in the project's `downloads/`, which is always checked. There a file counts when it has the expected name or a browser's duplicate of it, like `name (1).jar`, newest first, or when it arrived after the wait started. Each is checked by hash. It is copied into `downloads/` under its expected name if it was there before the wait, and moved if it arrived during it. A file with the expected name and the wrong bytes stays where it is, and the checklist says so |
| `eula` | `true` accepts the [Minecraft EULA](https://aka.ms/MinecraftEULA) for every project, so server builds write `eula.txt` and `serve` doesn't ask. A build never writes over an `eula.txt` shulker didn't write. A manifest can't accept it for you |
| `registry` | The file listing linked instances and synced directories: absolute, or relative to the directory holding `config.json`. Without it, `registry.json` beside `config.json` |
| `instances` | Where [`shulker link shulker`](#shulker-link-shulker) puts the instances shulker owns. Without it, `instances` in shulker's data directory |
| `saves` | Where the save groups those instances share live. Without it, `saves` in the same data directory |
| `store` | Where a launch assembles its shared versions, libraries and assets. Without it, `game` in shulker's cache, since it holds only what shulker can fetch again |

The CurseForge key is always shown as its last four characters, like `••••c123`, unless you pass `config get --reveal`. With `--json`, `config set` and `config unset` return `{ "path", "from", "to" }` like `set`, plus `created` when they made a new registry file.

### `shulker config get`

Print a key: a string as it is, anything else as JSON. With no key, print all of `config.json`. `registry`, `instances`, `saves` and `store` show the path shulker actually uses, even when the key isn't set, and `accounts.stores`, `downloads.watch`, `play.saveBackups`, `log.keepDays` and `security.minReleaseAge` show their defaults the same way. A `curseforge.key` that isn't set fails with `path-not-set`.

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
shulker config set accounts.stores --literal '["shulker","prism"]'
```

A key that holds a list needs `--literal`, which reads the value as JSON. `accounts.stores` is checked as it is set: it must be a non-empty array of launchers shulker reads accounts from, with no repeats, so a typo fails here rather than on the next run. The `play.` keys are checked the same way, against the rules of the instance setting of the same name. Every value is checked against `config.json`'s schema before anything is written, so a `--literal` of the wrong type fails with `usage`, and `accounts.default` must be the id of an account shulker can see, or it fails with `account-not-found`.

`set` is the one command that tolerates a `config.json` it can't read, one that fails `config-invalid` or `schema-newer` everywhere else: it renames that file to `config.json.replaced`, warns naming why it couldn't be read, and writes a config holding only the key being set. Only the latest replaced config is kept.

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

A `shulker.local.json` shulker can't read never stops a command. When it isn't valid JSON, names no `$schema` or another file's, or was written by a newer shulker, shulker renames it to `shulker.local.json.replaced`, warns naming both paths, and goes on with the declared defaults. The next `feature on` or `off` writes a fresh file. For a newer file, the warning says to run `shulker self update` and move it back.

A directory you sync into, such as a launcher instance, can have its own choices in its own `shulker.local.json`. Set them with `--into <dir>`, or with `-i <id>` for anything [`shulker instances`](#shulker-instances) lists. When you sync into it, its choices beat the project's, and `--with` and `--without` still beat both.

### `shulker config path`

Print where `config.json` is, whether or not the file exists yet: `$SHULKER_CONFIG` when set, else the platform's config directory. [`shulker cache info`](#shulker-cache-info) prints the cache's location and [`shulker config get registry`](#shulker-config-get) the registry's; [`shulker version --verbose`](#shulker-version) shows them together.

```sh
shulker config path
```

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
| `--launcher <launcher>` | Only match `-i` against instances linked in this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
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
| `--launcher <launcher>` | Only match `-i` against instances linked in this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
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
| `--launcher <launcher>` | Only match `-i` against instances linked in this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--side <side>` | Only match `-i` against `client` or `server` instances |

## Builds

### `shulker install`

Download everything in the lock and build every side the manifest declares. Run this after cloning a project. A locked file its author blocks from third-party download is named with its page, and the project's `downloads/` is created for it. A mod an import left pending is asked for the same way, matched by its sha1, and the lock takes its `sha512` once it is there. At a terminal the command then waits on a checklist of the files: it reads the folder, and the folders [`downloads.watch`](#configuration) names, every 5 seconds, or at once on Enter, matching each file by hash whatever its name, and goes on by itself once every one is there. A file dragged into the terminal, or its path pasted, is taken the same way. Esc skips the files still missing, which the build leaves out with a warning until an `install` finds them. Off a terminal, or with `--no-input` or `--json`, pending mods are left out the same way and any other missing file fails `missing-files`. `sync` and `play` wait the same way at a terminal; `serve`, `export` and a launcher's hook never wait, since a wait would hang a launch.

```sh
shulker install
```

| Flag | Description |
| --- | --- |
| `--force` | Overwrite files edited in the build directory, seeded files included |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |
| `--fail-fast` | Stop at the first file that fails to download, rather than trying them all |
| `-v, --verbose` | Print a line for every file fetched, rather than one count per group |

### `shulker build`

Assemble a side's build directory from the lock and its overrides. With no side, builds every side the manifest declares.

A file you edited in the build directory is kept until the source changes it too. Then it is a conflict, and so is a file in the way that shulker never wrote: the build fails `build-conflict` before writing anything, listing each one. `shulker diff` shows them, `--force` takes the source's version, and `shulker pull` copies yours into the project instead. `build`, `sync` and `install` all stop this way; only a sync for a launch, from [`play`](#shulker-play) or a launcher's [pre-launch hook](#shulker-hook-pre-launch), keeps your file and applies the rest.

A build asks no provider anything, but it repeats the warning of the directory's last [takedown check](#shulker-sync) for every file the lock still holds.

A jar shulker placed in `mods/` that has changed since, whether a mod updated itself, you edited it, or something worse rewrote it, stays as it is, like any file you changed. It isn't listed as kept: the build warns that it no longer matches the copy shulker locked, without guessing why, and names `shulker audit <key>` to look at it and `--force` to put the locked copy back. With `--json` these are `changedJars` (`path`, and `key` for a locked mod), apart from `kept`, and the warning is in `securityWarnings` with protection `placed-jars`.

A file the manifest's `seedFiles` lists is never a conflict. When both changed it, every build keeps yours and warns `<path> changed in the pack and in game; kept yours`, naming the two ways to take the pack's: delete the file, or `--force`, which resets every seeded file along with the rest. A file merged per key, such as `options.txt` from `client.options` or a `.properties` override, is seeded key by key: the warning names the keys you changed, and the rest follow the pack.

```sh
shulker build
shulker build client
```

| Flag | Description |
| --- | --- |
| `--force` | Overwrite files edited in the build directory, seeded files included |
| `--accept-player-change` | Relock a player name that now belongs to a different account |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |

### `shulker diff`

Show what was edited in a build directory since `build` wrote it, such as config changed in-game, as a diff from the project to the directory. `build` leaves these files alone and `pull` copies the edits back. A per-key file shows only its managed keys. With no side, checks every side the manifest declares.

A seeded file, one the manifest's `seedFiles` lists, shows up like any other: `kept` when only you changed it, `conflict` when the pack did too, marked "seeded, the build keeps yours", since the build keeps it rather than failing. Its diff is from the pack's current version, so it shows what you aren't getting. With `--json`, each entry is `{ "path", "state", "seeded", "diff" }`, `seeded` set only for a seeded file.

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

A bare `pull` leaves seeded files out, so a pack author's own settings don't become the pack's defaults, and lists each under `skipped` as `<path> (seeded; name it to pull)`. Naming the file, with or without `--key`, pulls it as any other.

A named file that is a mod or a pack the game loads, a `.jar` directly under `mods/` or a `.zip` directly under `resourcepacks/`, `shaderpacks/` or a datapack folder (`datapacks/`, `config/paxi/datapacks/`, `config/openloader/data/`, `config/openloader/packs/`), is adopted as a `file` entry instead of an override: copied into `files/`, written into `requires` with no conditions, and locked. Its key is the jar's mod id or the pack file's name; a jar's side is the one the jar declares. A pack whose file name isn't `<key>.zip` gets that name as its `filename`, so builds keep placing it under the name the game already enables it by. A key `requires` already holds skips the file, and `--as` names another. The result's `entries` lists the adopted files.

```sh
shulker pull
shulker pull config/sodium-options.json --side client
shulker pull config/iris.properties --key colorSpace
shulker pull mods/private-mod-1.4.jar --as private-mod
```

| Flag | Description |
| --- | --- |
| `--side <side>` | Side whose build directory to pull from (default: the only declared side) |
| `--into <path>` | Directory the side was synced into (default: the build directory and every directory `sync` recorded) |
| `--key <key>` | Start managing this key of the one named `.properties` file, copying its current value into the override; repeat for more |
| `--as <key>` | Key in `requires` for the one named jar or pack adopted as a `file` entry (default: a jar's mod id, a pack file's name) |
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

List every account shulker can see, grouped by where it came from: **Own** for the ones signed in through Microsoft, **Offline** for the ones `accounts add` created, and **Launcher** for the ones read from another launcher. Each row carries the account's id and its state, and the default account's row is marked `✔` instead of `•`.

The id is a player UUID, or an Xbox user id for an account that owns no Java profile and so has no UUID. It is on every row because it is what tells two accounts with the same name apart, and what you type to pick one.

```sh
shulker accounts
```

The states are `playable`, `not playable (no Java profile)`, `sign-in expired`, `token expired <ago>` for a launcher account whose launcher has not renewed it, and `offline`.

Which accounts are read comes from `accounts.stores`. With `--json`, each row is `{ "id", "name", "source", "group", "state", "default" }`.

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

Shulker asks before it signs anything out, and `--yes` answers the question ahead of time — which a run that isn't on a terminal has to pass, since there is nobody to ask. Signing out the default account leaves the one account a launch could still use behind it as the default — offline or from a launcher as readily as signed in — and no default at all when there isn't exactly one to take over.

An offline account has no sign-in to end, so `logout` on one is an error naming [`shulker accounts remove`](#shulker-accounts-remove); a launcher account belongs to the launcher it came from, and only that launcher can sign it out.

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

Creating one needs an account in sight that owns Minecraft: Java Edition, own or from a launcher: any with a Java profile, even one whose sign-in or session token has run out. It is a statement of intent rather than a licence check, and it is checked only here: afterwards the offline account plays on, and stays the default, with every Microsoft account signed out.

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

With no account in sight that owns Java Edition, removing one is refused with `ownership-unproven`, since the same gate would block creating it again; `--force` removes it anyway. A Microsoft account is signed out rather than deleted, so `remove` on one is an error naming [`shulker accounts logout`](#shulker-accounts-logout), and a launcher account belongs to the launcher it came from.

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

An account is named by its username or its id, dashed or not, or by the start of either, and `name@source` narrows it to one source: `shulker`, `offline`, or a launcher in `accounts.stores` — `prism`, `multimc`, `mojang`, `atlauncher` or `gdlauncher`. A name may hold spaces, because an account with no Java profile is named by its Xbox gamertag, so quote it. When several accounts match, shulker takes the closest and warns which one it chose: the full id, then the full name with its case, then without it, then the start of a name with its case, then without it, then a shorter name before a longer one. So with offline accounts `Steve` and `steve`, `ste` picks `steve` and `Ste` picks `Steve`. The start of an id counts only when no name starts that way, and only when it starts one id. When the input can't tell accounts apart, because they share the very same name or their ids share the prefix typed, shulker asks which one on a terminal, and fails with `ambiguous-account` anywhere else, listing each with its qualifier and its id. An account that owns no Java profile can't launch anything, so it is refused with `account-not-playable`.

### `shulker accounts stores`

List the launchers shulker reads accounts from, in the order it reads them. The list is `accounts.stores` in `config.json`; without it, shulker reads only its own accounts. Each store is shulker itself or a launcher shulker has a reader for.

```sh
shulker accounts stores
```

The order is what settles a Microsoft account signed in to more than one launcher: it is listed once, from the earliest store that has it. With `--json`, the list comes back as an array of names.

Where a launcher's accounts are read is the directory a registered instance of it was linked against — the one named with [`shulker link prism --launcher-dir`](#shulker-link-prism) — and the launcher's usual directory on this machine otherwise. Prism's accounts come from `accounts.json` there, Microsoft and offline accounts alike, and MultiMC's from the same file, which holds only Microsoft accounts; MultiMC has no usual directory, so its accounts are read once an instance of it is linked. ATLauncher's come from `configs/accounts.json`, Microsoft accounts only, and GDLauncher's from the `gdl_conf.db` database in its data directory, Microsoft and offline accounts alike, including a sign-in made while GDLauncher is still open. The Minecraft Launcher's come from both `launcher_accounts.json` and `launcher_accounts_microsoft_store.json` in [its own directory](#shulker-link-mojang), because that suffix is per file rather than per install; an account in both is listed once, with the session that lasts longer. Shulker opens neither the entitlements file beside them nor the launcher's stored credentials: an account's Java profile is its own proof that it owns the game, and an account with no profile isn't listed at all, since its username and UUID both live there. Shulker never renews a launcher account and never writes to another launcher's files: a launcher account's session token that has run out is shown as `token expired <ago>`, still plays, and warns at launch that online servers and Realms will reject it until that launcher renews it. A file shulker can't read warns, naming itself, and is skipped, so a corrupt one can't take the account list down — the other accounts file in the same directory still loads.

### `shulker accounts stores add|remove`

Add a launcher to the list, or take one out.

```sh
shulker accounts stores add prism
shulker accounts stores remove shulker
```

Both print the list before and after. Adding a launcher already in the list, or removing one that isn't in it, changes nothing and says so. A launcher shulker can't read accounts from and a change that would leave the list empty are both usage errors — unset `accounts.stores` to go back to the default instead of emptying it. Adding a launcher that isn't installed warns once, naming the directory that was checked; it is not an error, and nothing says it again afterwards.

### `shulker accounts stores set`

Replace the whole list, in the order given.

```sh
shulker accounts stores set shulker prism
shulker accounts stores set prism
```

The same checks apply, and a launcher new to the list warns about a missing directory the way `add` does. The generic path is `shulker config set accounts.stores --literal '["shulker","prism"]'`, which checks the value the same way.

## Running

### `shulker play`

Start a shulker instance. With no nickname it plays the instance the current directory is; `-i` names one from anywhere.

In a project folder, `play` plays the instance shulker owns for that project, the one [`shulker link shulker`](#shulker-link-shulker) made from it. Instances other launchers own are synced from the project too but never count, since shulker doesn't start them. With several, it asks which on a terminal, and off one it fails with `ambiguous-instance`, listing them to pass with `-i`. With none, it asks on a terminal to create one, which is what `link shulker` does, and then plays it; declining, or running under `--no-input` or `--json`, fails with `instance-not-found`. `--dry-run` never creates one.

It updates the instance first, resolves the account, fetches whatever the store is missing, and then starts the game **detached**: `play` returns as soon as the game is running, and the game outlives the shell it was started from. `--no-sync` starts what is already on disk without updating it, and so does every `play` of an instance whose `hooks.preLaunch` is `false`. A file you edited that the update also changes never holds the game back: the update keeps your file, applies the rest, and warns with the sync result how to take the pack's version, as [`sync`](#shulker-sync) describes. Once a launch has filled the store, `play` needs no network: a sync that can't reach the source warns and builds from the lock the instance already has, and the game, its loader and its libraries come from the store. A sync that one of shulker's protections refuses, such as `provenance-mismatch`, warns with how to fix it and starts the build already on disk; with no build there yet, `play` fails with it.

A detached game is still recorded. `play` hands it to a watcher — shulker itself, started again in the background with no window — which starts the game, waits for it, writes how the run ended to the instance's launch history, and exits. The record is the same one a launcher's [post-exit hook](#shulker-hook-post-exit) writes, so `instances` and the history read it the same way, except that shulker started the game itself and so also knows the status it exited with: a non-zero status is `crashed` whether or not the game managed to write a crash report. If the watcher is killed while the game is running, the record stays open with the game's process id in it, and the next command that touches the instance — `play`, `sync`, `instances repair` — closes it from the crash reports once that process has gone.

A detached launch prints the account, the log and the game's `pid`, then a hint: if the game hangs, [`shulker instance dump`](#shulker-instance-dump) shows where it is stuck, and [`shulker instance log`](#shulker-instance-log) prints its output.

`--wait` keeps the launch in the foreground instead: `play` starts the game itself, waits for it, records the run and says how it went. `--stream` does the same and shows the game's output as it runs, on stdout, or on stderr under `--json`. The log is written either way. A game that crashes is reported, not raised: `play` exits 0, because it got the game running, which is its job.

Only the instances shulker owns can be played here: every other launcher starts its own, so an instance linked into one fails with `not-shulker`.

```sh
shulker play
shulker play smp
shulker play --account Notch
shulker play --no-sync
shulker play --wait
shulker play --stream
shulker play --world "New World"
shulker play --server mc.example.com:25565
```

The game gets no terminal, so everything it writes goes to `.shulker/logs/<time>.log` inside the instance, one file per launch, whether or not anyone is watching. The game's own arguments carry a session access token, so they are printed nowhere: not in the log, not in a progress line, not in an error.

Who plays is the instance's pinned `account` when it has one, and otherwise the default account; `--account` names another for a single run, matched the way [`shulker accounts use`](#shulker-accounts-use) matches one. [`shulker instance set account`](#shulker-instance-set) pins one. A pinned account that has since been removed fails with `account-not-found` rather than playing as someone else. With no default account shulker takes the only account that could play and makes it the default, saying so; with several it asks on a terminal and records the answer, and off one it is a usage error naming `--account`. With no account at all it is `no-accounts`. An account whose sign-in has expired refuses the launch with the line that fixes it, and one playing on a token shulker couldn't renew — one from a launcher that has let it run out, or a cached one with no network — launches with a warning that online servers and Realms will reject the session.

The launch takes its memory, extra JVM arguments, Java, window size and wrapper from the instance's settings, and each one the instance leaves out from its `play.` default in `config.json` — see [`shulker instance`](#shulker-instance). The heap has two more steps: the pack's `client.memory` in `shulker.json`, which its author sets to what the pack needs, and then `4G`, the default other launchers use, so the game never gets the JVM's own guess of a quarter of the machine's RAM. `memory` becomes `-Xms` and `-Xmx`, and it and `jvmArgs` go after the version's own JVM arguments, so they win over them. The window becomes `--width` and `--height`, which the game takes for the run and never writes back; `--window` sets it for one run over both, and nothing is saved. Fullscreen isn't a launch setting: the game keeps it in `options.txt`, which the manifest's `client.options` owns. A wrapper runs the launch as its own command, with java and its arguments after the wrapper's.

`--world` boots straight into a save, named by its folder in the instance's `saves/`, and `--server` straight into a server, as an address with an optional port. Only one can be given: the game boots into one target. They go to the game as its quick play arguments, `--quickPlaySingleplayer` and `--quickPlayMultiplayer`. A Minecraft before 1.20 has no quick play, so there `--server` joins through the older `--server` and `--port` pair, on port 25565 when none is given, and `--world` fails with `unsupported-quickplay` before anything is launched: there is no older way to boot into a save, and a game that opened on the title screen instead would look like one whose save failed to load.

`--dry-run` assembles the launch and prints it instead of starting the game: the version it resolved and what it inherits from, the libraries a loader's version brings on top of that one as a count and a size, the Java it would use, the heap, the game and natives directories, the asset index, and the classpath as a count and a size. A second run downloads nothing. It needs no account, which is what makes the plan checkable on its own.

Everything a launch shares lives in the store: `versions/`, `libraries/` and `assets/` under the store root, laid out the way the Mojang launcher lays out its own directory. Move it with `shulker config set store <path>`. A Fabric or Quilt pack's loader comes from the loader's own metadata. A NeoForge or Forge pack runs the loader's installer against the store once per loader version, after shulker has fetched the vanilla client jar the installer patches. The natives a launch unpacks are per-instance and sit in `.shulker/natives`.

```sh
shulker play --dry-run
shulker play smp --dry-run
```

| Flag | Description |
| --- | --- |
| `--account <name>` | Play as this account, for this run only (default: the instance's pinned account, else the default account) |
| `--window <w>x<h>` | Open the game at this size for this run only, like `1280x720` (default: the `window` setting) |
| `--world <save>` | Boot straight into this save, named by its folder in `saves/`; needs Minecraft 1.20 or later |
| `--server <address>[:<port>]` | Join this server straight away; can't be combined with `--world` |
| `--no-sync` | Start the game without updating the instance first, as `hooks.preLaunch` set to `false` does every time |
| `--wait` | Wait for the game and record how the run ended before returning |
| `--stream` | Wait for the game and show its output as it runs; the log is still written |
| `--dry-run` | Assemble the launch and print it instead of starting the game |
| `-y, --yes` | Create a shulker instance for a project that has none without being asked first |
| `-v, --verbose` | Print a line for every file fetched, rather than one count per group |

With `--json`, the data is `{ "instance", "version", "account", "pid", "gameDir", "log", "outcome", "exitCode", "crashReport", "sync" }`, where `account` is the row [`shulker accounts`](#shulker-accounts) prints, `pid` is the game's own process, and `sync` is absent under `--no-sync`. `outcome` (`ok` or `crashed`), `exitCode` and `crashReport` are there only under `--wait` or `--stream`, since a detached launch returns while the game is still running; `exitCode` is absent when it is 0, and `crashReport` when the game wrote none. Under `--dry-run` it is `{ "instance", "version", "inherits", "mainClass", "java", "gameDir", "nativesDir", "assetIndex", "classpath", "classpathBytes", "loaderLibraries", "loaderLibrariesBytes" }` instead, where the two `loaderLibraries` keys are absent for a version that inherits from nothing.

### `shulker serve`

Build the server side and run it in the foreground. It downloads whatever the lock needs first, the way `install` does, so a fresh clone reaches a running server in one command.

A server build that already holds an `eula.txt`, written by hand or copied from an override folder, runs as it is: nothing is asked or recorded, and no build writes over it. Otherwise, until you accept the Minecraft EULA, `serve` asks `Accept and record "eula": true in your shulker config?` on a terminal, with No preselected, and Yes records it in `config.json` before the server starts, so no project asks again. Off a terminal, or with `--no-input` or `--json`, nothing is asked and it fails with `eula-required` unless `--yes` is passed. `shulker config set eula true` accepts it ahead of time.

```sh
shulker serve
shulker serve server --yes
```

| Flag | Description |
| --- | --- |
| `--force` | Overwrite files edited in the build directory, seeded files included |
| `-y, --yes` | Accept the Minecraft EULA and record it in config.json without prompting |
| `--fail-fast` | Stop at the first file that fails to download, rather than trying them all |

### `shulker link`

Point a launcher at a pack. Name the launcher as a subcommand below. At a terminal, a bare `shulker link` asks `Which launcher?` over every launcher below, Shulker included, then carries on exactly as naming that launcher would. Under [`--no-input`](#global-flags), and so off a terminal or with `--json`, it prints this group's help instead.

```sh
shulker link
```

Every `link <launcher>` follows the source it is given, or else the project in the current directory. With neither, a terminal asks what `shulker init` would, in this order: whether to start from an existing pack, which is then followed as if it had been named; otherwise the Minecraft version, whether to add mods, the loader and its version, and what to call the instance, defaulting to what it runs, like `Fabric 26.2`. The instance directory then becomes a project of its own. It follows no pack, so `shulker add` inside it is how it grows, and the current directory is left alone. `--name` or `--as` answers the name question. Off a terminal, a link with no source outside a project fails with `manifest-not-found`.

### `shulker link shulker`

Create an instance shulker owns, under its own instances root, and register it like any other launcher's. No launcher is involved: shulker is the launcher here, so it runs the pre-launch sync and the post-exit record in process, writes no hook scripts, and fills no command slot.

With no source, it links the project in the current directory. Pass a project directory, git URL, or manifest URL to link that instead. The instance is a project of its own: a `shulker.json` that follows the source as a modpack and builds where it stands, so `shulker add` in the instance directory puts a mod on top of the pack and keeps it across syncs. shulker builds it before `link` returns.

The instance's nickname names both the folder under the instances root and the id `-i` takes, so the two can never drift. Without `--as` it comes from the pack's name. `link` never writes over a `shulker.json` that is already in the instance directory: it adopts that project, clears the unlinked mark, re-registers it and builds it where it stands, so relinking an instance you unlinked picks it back up with everything you added on top of the pack. If an instance of that name already follows a different modpack, `link` fails rather than repointing it: pass `--as` to name a second instance, or `--force` to repoint the modpack it follows.

Move the instances root with `shulker config set instances <path>`; `shulker config get instances` prints where it is now.

`--with` and `--without` are saved in the instance's own `shulker.local.json`. Change them later with `shulker feature on|off --into <instance folder>`, or run `link` again with new flags.

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
| `--path <path>` | Folder of a git source's repository that holds its shulker.json (default: the root) |
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

### `shulker link atlauncher`

Create an ATLauncher instance that syncs the client build before each launch.

shulker writes the instance itself: the Minecraft version, the loader if the project has one, and a pre-launch command that runs `shulker sync`. ATLauncher downloads the game, its libraries and Java the first time you press Play. For NeoForge and Forge, shulker runs the loader's installer once per loader version and copies what it builds into ATLauncher's `libraries` folder. A new instance shows the pack's `icon`, fitted onto ATLauncher's 300×150 card, or the shulker image when the pack names none. A sync replaces the image only when the pack's icon changes, so one you pick in ATLauncher stays until then. ATLauncher only reads its instances when it starts, so restart it if it is open.

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
| `--path <path>` | Folder of a git source's repository that holds its shulker.json (default: the root) |
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

shulker writes the instance's `instance.json` itself: the Minecraft version, the loader if the project has one, and a pre-launch hook that runs `shulker sync`. GDLauncher downloads the game, the loader and Java the first time you press Play, NeoForge and Forge included. Every `link` marks the instance for setup again, so the next Play re-checks the install and takes a little longer. It can only install loader versions on its own list, which trails new releases by a few days. When the locked loader version isn't on that list yet, the instance uses the newest one GDLauncher has and `link` warns you; run `link` again once GDLauncher adds it, or pass `--force` to use the locked version anyway. A new instance shows the pack's `icon`, or the shulker icon when the pack names none. A sync replaces the icon only when the pack's icon changes, so one you pick in GDLauncher stays until then. GDLauncher only reads its instances when it starts, and while open it writes its own copy back over them when you change settings or play, so quit it before linking and open it afterwards. On macOS and Linux, `link` and `unlink` warn you when GDLauncher is open.

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
| `--path <path>` | Folder of a git source's repository that holds its shulker.json (default: the root) |
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

`--with` and `--without` are saved in the instance's own `shulker.local.json`. Change them later with `shulker feature on|off --into <game dir>`, or run `link` again with new flags.

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
| `--path <path>` | Folder of a git source's repository that holds its shulker.json (default: the root) |
| `--force` | Repoint the modpack a profile already follows |
| `--no-hooks` | Install neither hook: don't sync before a launch, don't record how a run ended |
| `--no-pre-launch` | Don't sync this instance before each launch |
| `--no-post-exit` | Don't record how each run ended |
| `--no-marker` | Leave the marker mod out of this instance's builds |
| `--with-marker` | Include the marker mod in this instance's builds, over a manifest that leaves it out |
| `--java <path>` | Absolute path to the Java this machine launches the instance with (default: shulker's managed runtime) |
| `--wrapper <cmd>` | Command prefix for the launch command, such as `gamemoderun`; split on whitespace |
| `--with <feature>` | Turn a feature on for this instance; repeat for more |
| `--without <feature>` | Turn a feature off for this instance; repeat for more |

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
| `--path <path>` | Folder of a git source's repository that holds its shulker.json (default: the root) |
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

Create a MultiMC instance that syncs the client build before each launch. It is [`shulker link prism`](#shulker-link-prism) for MultiMC's own `instance.cfg` dialect, with the same source argument, flags and behaviour, and one difference: MultiMC is portable and has no fixed data folder, so `--launcher-dir` names the folder that holds `multimc.cfg`. A terminal asks `Where is MultiMC installed?` when it is missing; under `--no-input` it is required (`launcher-dir-required` without it). The instance is registered under the launcher name `multimc`, which is what `--launcher multimc` and `shulker unlink multimc` match.

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
| `--path <path>` | Folder of a git source's repository that holds its shulker.json (default: the root) |
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

Download and build one side of a project straight into a directory, without setting up a project there. The source can be a project directory, a git URL, or a manifest URL. A manifest URL brings only that `shulker.json` and the `shulker.lock` beside it, never the project's overrides or local files, so a sync from one warns, naming any feature override folders and icon its manifest points at, and fails `source-incomplete` when it has a local `file` entry; sync from the repository's git URL, with `--path` for a project in a subfolder, to get them. Worlds, logs, screenshots and crash reports stay in the directory you sync into, and nothing is written into the source project; only the project's own build directories link them to its `data/<side>/`. Before a sync adds, replaces or removes a mod, it backs up the directory's worlds, as [`shulker backup`](#shulker-backup) would, with the reason `sync`; `play.saveBackups` in [Configuration](#configuration) says how many it keeps. A source from a URL installs its lock as its author locked it whatever [`security.minReleaseAge`](#configuration) says, since those are the versions the author tested, and warns with each entry published more recently than that, how old it is and the day it qualifies; under `--json` they are in `securityWarnings`, as `data.young`. A hosted modpack the project floats is chosen under the setting, as `update` chooses one.

```sh
shulker sync https://github.com/shulker-sh/base-pack.git --side server --into /srv/minecraft
shulker sync ../my-pack --side client --into ~/instances/my-pack
shulker sync -i friends-smp
shulker sync --into ~/instances/my-pack
shulker sync --all --side server
shulker sync
```

With `--into` and no source, shulker reads what the directory syncs from out of its own `.shulker/instance.json`, so a synced directory keeps working even if the registry is gone.

If a git or manifest URL can't be reached because the network is down, `sync` warns and builds from the copy used by the last sync from that source that succeeded, so an instance still launches offline. The warning names the commit and says how old that copy is. A server that answers with an error, a missing ref, or a failed login still fails the sync, and so does a source that has never synced successfully here. A modpack the project requires that can't be reached stays at the version in `shulker.lock`, with a warning naming it, while every other modpack still updates. `--offline` skips the network entirely, which is quicker than waiting for timeouts on a network that drops traffic. For the server side, an installed Java runtime is kept when its update check can't reach the network.

Once a day per directory, `sync` asks Modrinth and CurseForge whether they still have each file the lock holds, as [`audit`](#shulker-audit) does, and records what they said in the directory's `.shulker/state.json`. A file gone from its provider, or one its provider now files under another project, is a warning rather than a failure, and the cached copy is still placed: the warning names each file, the registered instances that use it, and the `audit`, `update` and `remove` that deal with it. Offline, or when no provider can be asked, the check is skipped without a word and runs at the next sync. Under `--json` the warning is in `securityWarnings`, with protection `takedowns`.

A sync that brings a change you should see before playing lists it first: a mod or pack it adds from a provider, a file no provider published (a lock entry with a URL or local file of its own, or a jar or pack an override folder lays in `mods/`, `resourcepacks/`, `shaderpacks/` or a datapack folder), and an entry now locked from another provider project than before. At a terminal `sync` asks whether to apply them, Yes preselected; answering no leaves the directory as it was and exits 0, and `--all` carries on with the others. `--no-input`, `play`, `link`, the [launcher hooks](#shulker-hook-pre-launch) and [`hook wrap`](#shulker-hook-wrap-java-arguments) apply them without asking and warn, which the command log keeps. A directory's first sync has nothing to compare against, so it asks nothing. With `--json`, `data.review` holds the changes (`added`, `unpublished` and `moved`, each with the entry's `key` and `type`, or an override file's `path`, and `provider`, `project`, and `wasProvider`, `wasProject` for a moved one) and `data.declined` is set when you said no; the warning is in `securityWarnings` with protection `sync-review`.

On the client, the pack's own entry in the in-game mod list lists what shulker wants you to know about the instance: a Notices section for files gone from their provider and jars changed since shulker placed them, and one section per day of the last 30 that a sync brought changes on. Each appears only while it has something to show.

A directory `sync --into` fills gets a `.shulker/instance.json` recording what it syncs from. An index of the instances shulker keeps in sync lives in `registry.json` beside shulker's `config.json` (a `registry` path in `config.json`, relative to that file, moves it), and [`link`](#shulker-link-prism) is what adds to it. A `sync --into` directory is a detached build, not an instance: it takes no row, so it never shows up in [`shulker instances`](#shulker-instances). What records it is the source project's `shulker.local.json`, which is where [`diff`](#shulker-diff) and [`pull`](#shulker-pull) find it. Inside that project, `sync` lists it marked `detached build` with its directory, under an id taken from its folder name (`~/packs/fo-test` is `fo-test`, suffixed only when an instance or another detached build already holds that id), and `-i` takes that id there.

To update an instance, name it instead of a source. `-i` takes an instance's id, its name, or its directory, and syncs it from the modpack its `shulker.json` follows. Ids are unique, so `-i <id>` always picks exactly one; a name several instances share needs `--launcher` or `--side` to narrow it, or `--all` to sync them all. `--all` alone syncs every instance. It keeps going when one fails, and exits with an error at the end. With no source and neither flag, `sync` run inside a project syncs every instance synced from that project and every directory it has synced into, narrowed by `--launcher` or `--side`. Outside a project it asks which one to sync when run in a terminal, and fails with the list otherwise.

A project whose side builds into its own directory is an instance, and `sync` run inside it (or naming it with `-i`) updates the instance itself first: modpacks that follow their source are fetched again (every modpack except one set to `"autoUpdate": false`), the lock is resolved against them without moving your own mods, and the side is built in place. Nothing is written, and no history entry is taken, when the lock comes out unchanged. Every instance synced from it is synced after, since those build from its lock. A modpack update your own mods can't satisfy stops the sync with the reason, leaving the lock and the directory as they were; a launcher's pre-launch hook instead builds what the lock already has and starts the game.

A file changed both in the directory and in the source fails the sync with `build-conflict`, as [`build`](#shulker-build) does, and `--force` takes the source's version. A sync for a launch is the exception: it keeps your file, applies everything else, and warns `kept your version of <file> (<why>); shulker diff shows the pack's`, with the forcing command beneath. From then on the file counts as edited in place, so the next launch says nothing more until the pack changes it again.

| Flag | Description |
| --- | --- |
| `--into <path>` | Output directory (default: the side's build directory) |
| `--all` | Sync every instance `-i` matches, or every instance when there's no `-i` |
| `--launcher <launcher>` | Only instances linked in this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--side <side>` | Side to build from a source (default: the only declared side); with `-i`, `--all`, or the picker, only `client` or `server` instances |
| `--offline` | Don't use the network; build from the last successful sync and cached files |
| `--force` | Overwrite files edited in the output directory, seeded files included |
| `--assume-client` | Build a client even when the source declares none, from the mods and overrides both sides share; recorded in the directory so later syncs keep building it |
| `--ref <ref>` | Branch, tag, or commit to sync from a git source (default: the remote HEAD) |
| `--path <path>` | Folder of a git source's repository that holds its shulker.json (default: the root) |
| `--os <os>` | Build for this OS instead of the detected one: `macos`, `windows`, or `linux` |
| `--with <feature>` | Turn a feature on for this run only; repeat for more |
| `--without <feature>` | Turn a feature off for this run only; repeat for more |
| `--fail-fast` | Stop at the first file that fails to download, rather than trying them all |
| `-v, --verbose` | Print a line for every file fetched, rather than one count per group |

### `shulker instances`

List the instances shulker keeps in sync in one table, sorted by launcher and then name. Each row leads with the instance's id, which is what `-i` takes, and shows its launcher, its side and when it was last synced. A directory that is gone or can't be read is flagged, and so is one missing its `.shulker/instance.json`. When the last sync failed, the row says so and names why. When the last launch never got as far as running the game, a line under the status says so and names the reason. `--verbose` adds each instance's directory, source and ref.

```sh
shulker instances
shulker instances -v
```

```
     Instance     Launcher            Side    Status
  ────────────────────────────────────────────────────────────
  •  friends-smp  Prism Launcher      client  synced 5 minutes ago
  •  my-pack      Minecraft Launcher  client  synced 2 days ago
```

| Flag | Description |
| --- | --- |
| `-v, --verbose` | Also print each instance's path, source and ref |

### `shulker instances repair`

Put the registry back in step with what is on disk. It works even when `registry.json` can't be read, rewriting it from what it finds: it scans each launcher's own instances directory and shulker's own instances root, registers any folder shulker syncs that isn't in the index and wasn't unlinked, and writes a `.shulker/instance.json` for any instance missing one or holding one it can't read. A registry or instance file it replaces, a newer shulker's included, is renamed to `registry.json.replaced` or `.shulker/instance.json.replaced` first, with a warning naming it. A registered directory that is gone is reported rather than dropped, since an unmounted disk looks exactly like a deleted instance; [`shulker unlink`](#shulker-unlink) is what forgets one. `shulker self update` runs it after a successful update.

A folder counts as one shulker syncs when it holds a `shulker.json` that builds where it stands — the project a [`link`](#shulker-link-prism) leaves in a game directory — or a `.shulker/` shulker wrote before, so an instance that lost its `.shulker/` is found by its manifest alone. Such an instance follows the one modpack that manifest requires, comes back under the id it was linked as, which the manifest's `name` records, takes its name from the launcher's own file, and gets an instance file holding only its settings, since the manifest holds the rest. A directory that is no project of its own reads what it syncs from in its instance file, or in what its last build recorded where that file is the part that went missing. An instance marked unlinked stays unregistered either way.

A row's name is the one its launcher shows, read from the file the launcher keeps it in, or the folder's name when there is none to read. Rename an instance in the launcher and repair follows: the row takes the new name and prints a `renamed` line, and its id stays, so `-i` and any script using it keep working.

A registered instance whose hooks are on but missing from its launcher, or left pointing at another shulker binary, is hooked again and listed under `Rehooked`; one linked with `--no-hooks` stays unhooked, and one whose launcher file is gone is a warning naming that file. A directory repair finds and registers anew gets no pre-launch command: it is listed and syncs with `-i`, and a [`link`](#shulker-link-prism) is what makes the launcher refresh it before each launch. After a rehook, restart the launcher if it's open before playing: Prism Launcher keeps its own copy of an instance's settings while it runs and writes that copy back at the next launch, taking the hook with it.

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

`instance get`, `instance set`, `instance unset` and `instance edit` read and change the settings of one instance: the one the current directory is, or the one `-i` names from anywhere. `instance dump` and `instance log` look into its game: where a running game is stuck, and what the latest run printed. That is its `.shulker/instance.json`, and every directory shulker syncs into has one, so an instance in another launcher has settings here too. The plural [`shulker instances`](#shulker-instances) is the list of them.

A path is relative to the file's `settings` block and dotted the way `shulker set` dots the manifest: `memory`, `hooks.preLaunch`. Five settings are launch settings with a default in `config.json` under `play.`, which [`shulker config set`](#shulker-config-set) sets for every instance at once: an instance that sets one wins, and one that doesn't inherits the default. They apply when shulker launches the instance itself, with [`shulker play`](#shulker-play). `play.java` and `play.wrapper` don't reach another launcher, but an instance's own `java` and `wrapper` still point that launcher at a Java and a wrapper, as before.

| Setting | Default | Description |
| --- | --- | --- |
| `memory` | `play.memory` | Heap size, as `-Xms` and `-Xmx`, like `6G`. Without either, the pack's `client.memory`, and without that `4G` |
| `jvmArgs` | `play.jvmArgs` | Extra JVM arguments, after the version's own and the memory, so one of them wins over both. An instance's list replaces the default rather than adding to it |
| `java` | `play.java` | Absolute path to a java binary, or for `play` a Java home. Without either, shulker's managed runtime |
| `window` | `play.window` | Window size, like `1280x720`. `play --window` wins over both for one run |
| `wrapper` | `play.wrapper` | A command the launch runs through, like `["gamemoderun"]` |
| `account` | | The account `play` launches this instance as, over the default account |
| `marker` | | Whether to include the marker mod, over the manifest's own `marker` |
| `hooks.preLaunch`, `hooks.postExit` | | Whether the instance syncs before each launch, in another launcher or under `play`, and whether another launcher records each run |
| `launchHistory` | | How many launch records to keep |
| `savesGroup` | | The save group whose worlds this instance shares, `default` unless set; `none` keeps them in the instance. See [`shulker saves`](#shulker-saves) |

### `shulker instance get`

Print a setting as it is in effect. `--verbose` adds where it came from: set in this instance, with the default it would return to; its `play.` default; or the setting's own default. A setting neither the instance nor `config.json` sets fails with `path-not-set`. With no path, print every setting the instance has, with the `play.` defaults it inherits filled in.

```sh
shulker instance get memory
shulker instance get window -i smp
shulker instance get
```

| Flag | Description |
| --- | --- |
| `-v`, `--verbose` | Also say where the value comes from |

With `--json`, the data is `{ "path", "value", "from", "default" }`, where `from` is `instance`, `config` or `default`, and `default` is what `instance unset` returns the setting to, absent when there is nothing to return to. With no path it is the settings object.

### `shulker instance set`

Set a setting in this instance, over its default. The value is checked as it is set, against the instance file's schema: a list needs `--literal`, and `java` must be an absolute path. `account` takes an account's name or id, matched the way [`shulker accounts use`](#shulker-accounts-use) matches one, and records its id, so the pin still holds after the player renames themselves; an account shulker can't see fails with `account-not-found`.

```sh
shulker instance set memory 8G
shulker instance set window 1920x1080
shulker instance set jvmArgs --literal '["-XX:+UseZGC"]'
shulker instance set account Notch
shulker instance set hooks.preLaunch false -i smp
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

### `shulker instance dump`

Take a thread dump of the instance's running game and print the stacks of its `main` and `Render thread` threads, then the log that holds the rest. A game that froze with nothing on screen is usually sitting in one of those two, in the frame of the mod that holds it up. Only a game [`shulker play`](#shulker-play) started can be dumped, since shulker knows its process; with none running, it fails with `game-not-running`.

On macOS and Linux it sends the game `SIGQUIT`, on which Java prints every thread's stack into the run's log and carries on, and waits up to 10 seconds for the dump to finish (`dump-timeout`). On Windows it runs `jcmd <pid> Thread.print` from the folder the game's `java` is in and appends the dump to the run's log; a runtime with no `jcmd` there fails with `jcmd-not-found`, naming the path it looked for, and a `jcmd` that prints no dump fails with `dump-failed`, quoting what it said. The Windows path hasn't been checked on a real Windows machine yet.

A game started through `settings.wrapper` is refused with `game-wrapped`, naming the wrapper's pid: that is the only process shulker knows, and a wrapper that runs Java as its child rather than becoming it would be killed by the signal. Find the java process under it yourself and pass its pid with `--pid`.

| Flag | Description |
| --- | --- |
| `--pid <pid>` | Dump this Java process instead of the one the run recorded, for a game started through a wrapper |

```sh
shulker instance dump
shulker instance dump -i smp
shulker instance dump --pid 48213
```

With `--json`, the data is `{ "pid", "log", "threads" }`, where `threads` is every thread in the dump as `{ "name", "stack" }`, in the order Java printed them.

### `shulker instance log`

Print the game's output from the instance's latest run: the log [`shulker play`](#shulker-play) wrote under `.shulker/logs/`, or while another launcher's run is going, the game's own `logs/latest.log`. With no run yet it fails with `run-not-found`.

| Flag | Description |
| --- | --- |
| `-f`, `--follow` | Keep printing new output until the game exits or you press Ctrl-C; not with `--json` |
| `--limit <n>` | Print only the last `n` lines; with `--follow`, the last `n` and then everything new, like `tail -n <n> -f` |

```sh
shulker instance log
shulker instance log -f --limit 50
```

With `--json`, the data is `{ "log", "lines" }`.

### `shulker unlink`

Stop syncing a linked instance and remove it from the list. Its files, worlds, and feature choices stay. For a Prism Launcher or MultiMC instance, `unlink` removes the pre-launch sync but keeps the instance. It leaves a pre-launch command alone if you replaced shulker's with your own. For the official launcher, it removes the profile but keeps the instance directory and the installed loader. The directory's `.shulker/instance.json` is marked unlinked, so [`shulker instances repair`](#shulker-instances-repair) doesn't register it again; linking or syncing into it clears the mark. `unlink` also drops the directory from its source project's `shulker.local.json`, so a bare `shulker sync` there no longer builds it. A `sync --into` directory is a detached build with no row on the list: name it by its directory, from anywhere, and `unlink` marks it unlinked and drops it from its source project's `syncDirs`, keeping its files, so `shulker sync <source> --into <dir>` picks it up again.

Name the instance by the id [`shulker instances`](#shulker-instances) shows, by the name its launcher shows, or by its directory. Inside a project, a launcher name (`shulker`, `mojang`, `prism`, `multimc`, `atlauncher`, `gdlauncher`) unlinks that project's instance in that launcher, the reverse of `shulker link <launcher>`; an instance actually called that name comes first. A name several instances share needs `--launcher`, `--side`, or `--all`, while an id always picks one. `unlink` prints the command that sets the instance up again.

```sh
shulker unlink mojang
shulker unlink "Friends SMP"
shulker unlink "My Pack" --launcher prism
shulker unlink --all --side server
shulker unlink ~/servers/smp
```

| Flag | Description |
| --- | --- |
| `--all` | Unlink every entry the name matches, or every entry when there's no name |
| `--launcher <launcher>` | Only entries linked in this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher` |
| `--side <side>` | Only `client` or `server` entries |

### `shulker saves`

Every instance [`shulker link shulker`](#shulker-link-shulker) makes shares its worlds through a save group: its `saves/` folder is a link (a directory junction on Windows) to the group's folder under the saves root, so every instance in a group lists the same worlds. Each joins `default`. Set another group with `shulker instance set savesGroup <name>`, or `none` to keep the worlds in the instance; the next sync relinks it and names the worlds the game now lists. Nothing is copied or merged: leaving a group leaves its worlds there. Joining one replaces an empty `saves/`, and one holding worlds is moved in as the group when the group has none; when both hold worlds, the sync warns and leaves `saves/` alone until you merge them by hand. Instances in other launchers keep their own worlds.

With no target, `saves` lists the groups with how many worlds each holds, its size, and when it was last backed up. With `-i`, `-C` or `--group`, it lists that target's worlds and its backups, newest first. A shulker instance in a group shows the group; any other directory shows the backups in its `.shulker/backups/` and the worlds where its game keeps them: its own `saves/`, `data/<side>/saves` for a separate-dir build, and for a server only the world `level-name` names, read from the manifest when it builds the server in place and from `server.properties` otherwise, `world` when neither sets it. A group's backups live in `backups/<group>` in shulker's data directory.

```sh
shulker saves
shulker saves -i smp
shulker saves --group default
shulker saves --all --launcher prism
```

| Flag | Description |
| --- | --- |
| `--group <group>` | Show this save group rather than an instance |
| `--all` | Show the worlds and backups of every registered instance, each save group once |
| `--launcher <launcher>` | Only instances linked in this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher`; narrows `-i` or `--all` |
| `--side <side>` | Only `client` or `server` instances; narrows `-i` or `--all` |

With `--json`, the list is `[{ "name", "dir", "worlds", "size", "lastBackup" }]`, and a target is `{ "group", "dir", "worldsDir", "worlds", "backups" }` with each backup `{ "n", "id", "path", "taken", "reason", "instance", "size", "worlds", "names", "minecraft", "loader", "loaderVersion" }`. A backup's zip comment is its record: the time, reason, instance, world count, world names, Minecraft version, loader and loader version all come from it, and it wins over the filename for the listing, the order, and which backups are automatic. A zip without shulker's comment falls back to its filename for the time, reason and instance, and counts the world folders in the zip; it has no `names`, `minecraft`, `loader` or `loaderVersion`. A row reads `on request` for a backup [`shulker backup`](#shulker-backup) took, and `before a restore` for one [`shulker restore`](#shulker-restore) took. The list ends with the `shulker restore <n>` that puts one back; the number is the one printed beside it.

`saves`, `saves prune`, `backup` and `restore` take the same `--all`, `--launcher` and `--side` as [`shulker sync`](#shulker-sync): `--all` runs over every registered instance, and `--launcher` and `--side` narrow it, or narrow the name `-i` gives; a server is `--side server`. Instances that share a save group act on it once, under a heading naming the group and its instances, and a group only one of them reaches is shown as that instance. Each target prints as it finishes, and the run keeps going when one fails, then fails with `saves-failed`, `backup-failed` or `restore-failed`. A target with nothing to act on, no worlds for `backup` or no backups for `restore`, prints why and doesn't fail the run. `saves --all` prints one section per target with worlds or backups, a save group headed by how many instances share it, then names the targets with neither on one line. With `--json`, the data is a list with one entry per target, `{ "group", "dir", "worldsDir", "instances", "ok", "skipped", "result", "error" }`: `instances` lists the ids that reach it, `result` is what the command gives alone, `skipped` says why nothing was done, and `error` is set when it failed.

### `shulker saves prune`

Delete all but the newest `--keep` backups of a save group or instance, whichever backup took them. `--keep` is required, so a prune always says how many survive. The target is the one `shulker saves` would show: `--group`, `-i`, `-C`, or the current directory.

```sh
shulker saves prune --group default --keep 3
shulker saves prune -i smp --keep 0
shulker saves prune --all --keep 5
```

| Flag | Description |
| --- | --- |
| `--keep <n>` | How many of the newest backups to keep (required) |
| `--group <group>` | Prune this save group's backups rather than an instance's |
| `--all` | Prune the backups of every registered instance, each save group once |
| `--launcher <launcher>` | Only instances linked in this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher`; narrows `-i` or `--all` |
| `--side <side>` | Only `client` or `server` instances; narrows `-i` or `--all` |

With `--json`, the data is `{ "group", "dir", "worldsDir", "pruned", "kept" }`.

### `shulker backup`

Zip a target's worlds into its backups. The target is the one [`shulker saves`](#shulker-saves) would show: `--group`, `-i`, `-C`, or the current directory, with the worlds found the same way. A save group's backups go in `backups/<group>` in shulker's data directory, named `<time>-<instance>-backup.zip` for the instance that took them, or `<time>-backup.zip` when `--group` names the group alone; any other directory's go in its own `.shulker/backups/` as `<time>-backup.zip`. A second backup in the same second gets `-2` after the time. The zip holds the world folders at its root and nothing else, and its comment records the time, the reason, the instance, the world count and names, and the Minecraft version, loader and loader version the directory was last built with, which `saves` and `restore` read in place of the filename. Names that wouldn't fit in a zip comment are left out of it, keeping the count. A backup taken this way is never pruned automatically; [`saves prune`](#shulker-saves-prune) deletes it.

`--world` narrows the backup to the worlds it names, by their folder names, the same names [`play --world`](#shulker-play) takes and `saves` lists; it can be repeated. A narrowed backup restores only the worlds it holds, and is never pruned automatically either. A server holds only its `level-name` world, so there `--world` can name only that.

A target with no worlds folder, or none in it, fails with `no-worlds`, naming the folder it looked in. A world open in a running game, whose `session.lock` the game or server holds, is zipped anyway under the warning `! <world> is open in a running game; its backup may be torn`; the backups `update` and `sync` take first warn the same way. A `--world` the target doesn't hold fails with `world-not-found`.

```sh
shulker backup
shulker backup -i smp
shulker backup --group default
shulker backup --world survival --world creative
shulker backup --all --side server
```

| Flag | Description |
| --- | --- |
| `--group <group>` | Back up this save group rather than an instance |
| `--world <world>` | Back up only this world, by its folder name; repeatable |
| `--all` | Back up every registered instance, each save group once |
| `--launcher <launcher>` | Only instances linked in this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher`; narrows `-i` or `--all` |
| `--side <side>` | Only `client` or `server` instances; narrows `-i` or `--all` |

With `--json`, the data is `{ "group", "dir", "worldsDir", "id", "path", "taken", "reason", "instance", "size", "worlds", "names", "minecraft", "loader", "loaderVersion" }`, where `worlds` counts the worlds and `names` lists them.

### `shulker restore`

Put a backup's worlds back into a target: the one [`shulker saves`](#shulker-saves) would show, `--group`, `-i`, `-C`, or the current directory. `n` is the number `saves` prints beside the backup, newest first, and defaults to 1. `--backup` names one exactly instead: a value with a path separator, or naming a file that exists, is the path of any zip of world folders, from this target's backups or anywhere else and named however it is; any other value is a backup's name in this target's backups, with or without `.zip`. So restoring into a different target is `--backup <path>` with that target's `-i`, `-C` or `--group`.

Before it writes anything, `restore` backs up the worlds the target holds, as [`shulker backup`](#shulker-backup) would with the reason `restore`, so a restore can itself be undone; that backup is never pruned automatically. A group restored with `--group` names no instance, so its backup is `<time>-restore.zip`. Each world folder in the zip, read from the zip's own entries, replaces the one of the same name whole: the old folder is removed and the zip's unzipped in its place, never merged file by file. Worlds the zip doesn't hold are left alone, and so are those `--world` leaves out: it narrows the restore to the worlds it names, by their folder names, and can be repeated. `--as` puts a world back under another folder name, and needs the restore to come down to exactly one world, because the zip holds one or `--world` names one; anything else is a `usage` error. A server takes only its `level-name` world from the zip, and fails with `world-not-found`, naming that `level-name`, when the zip doesn't hold it. Restoring another server's world, saved under its own `level-name`, takes `--as` with this server's `level-name`; `--as` on a server can name nothing else.

`restore` fails with `world-in-use`, listing each world, when a running game or server has open a world it would replace: the game writes a world it holds back at its next autosave, so a restore under it would undo itself. Save and quit to the title screen, or stop the server, first. A `session.lock` a crashed game left behind doesn't count. It also fails with `world-not-found` for a `--world` the zip doesn't hold, `backups-empty` when the target has no backups, `backup-missing` when there is no backup `n` or none by the name `--backup` gives, and `backup-invalid` for a zip that won't open or holds anything other than world folders, each with a `level.dat`, at its root.

`restore --all` puts each target's newest backup back, and a target with no backups is reported rather than failing the run. It takes neither `n`, `--backup`, `--world` nor `--as`, since a number, a name or a world means something different in each target; to restore anything but a target's newest backup whole, name its target. `backup --all` likewise takes no `--world`.

```sh
shulker restore
shulker restore 2
shulker restore -i smp --backup 20260918-203015-smp-backup
shulker restore --group default --backup ~/Downloads/worlds.zip
shulker restore --world survival
shulker restore --world survival --as survival-old
shulker restore --all --launcher shulker
```

| Flag | Description |
| --- | --- |
| `--backup <name\|path>` | Restore this backup, by its name in the target's backups or a zip's path |
| `--group <group>` | Restore into this save group rather than an instance |
| `--world <world>` | Restore only this world from the backup, by its folder name; repeatable |
| `--as <world>` | Restore the backup's one world under this folder name |
| `--all` | Restore the newest backup of every registered instance, each save group once |
| `--launcher <launcher>` | Only instances linked in this launcher: `shulker`, `prism`, `multimc`, `mojang`, `atlauncher`, or `gdlauncher`; narrows `-i` or `--all` |
| `--side <side>` | Only `client` or `server` instances; narrows `-i` or `--all` |

The output names the backup taken first, then `✔ restored 2 worlds from <id> » <folder>`, then `~ <world>` for each world replaced and `+ <world>` for one the target didn't have, with `(from <world>)` after a world `--as` renamed. With `--json`, the data is `{ "group", "dir", "worldsDir", "from", "snapshot", "worlds" }`: `from` and `snapshot` are backups as `saves` lists them (a zip that isn't shulker's has only `id`, `path` and `size`), `snapshot` absent when the target held no worlds to back up, and each world is `{ "name", "from", "replaced" }`, `from` the world's name in the zip and present only when `--as` renamed it.

### `shulker hook pre-launch`

What a launcher's own pre-launch slot runs. shulker writes the script that calls it into the instance's `.shulker/` folder and points the launcher at that, so there is no reason to run this yourself: outside a launcher slot it would sync whatever directory it was run in. It syncs the instance from its source before the game starts, and a failure never stops the game — the launcher plays what is already on disk. A file the player edited that the update also changes is kept rather than failing the sync, so the rest of the update still lands; the warning naming it, and the command that takes the pack's version, print where the launcher shows the hook's output.

A launcher that gives shulker no way to show a message gets a deadline instead, so a long update can explain itself rather than looking like a hang. That is GDLauncher only, which discards a hook's output when its own five-minute limit runs out.

| Flag | Description |
| --- | --- |
| `--deadline <duration>` | Stop the update after this long and say so, aborting the launch (default: no deadline) |

### `shulker hook post-exit`

What a launcher's own post-exit slot runs, recording how the run ended in the instance's `.shulker/launches.json`: when it started and finished, whether the game left a crash report, and where that report and the log are. `settings.launchHistory` in `.shulker/instance.json` is how many runs are kept — 5 by default, `-1` every one, and `0` none at all, which records nothing.

A run's `outcome` is `ok`, `crashed`, or `not-started` for one the game never began, which also carries the reason in `error` and has the same `startedAt` and `endedAt`. A run [`play`](#shulker-play) started also has `exitCode`, the status the game left, which no launcher passes to its post-exit slot; and while it is still going, `pid`, the game's own process, `java`, the runtime it runs on, and `wrapped`, true when `settings.wrapper` started it so `pid` is the wrapper's, all of which the record drops once it is closed.

### `shulker hook wrap -- <java arguments>`

What the Mojang launcher's `javaDir` shim runs in place of Java, with the instance in `-C` and the game's own arguments after `--`. When those arguments carry `--gameDir` it does what the pre-launch and post-exit hooks do around the game: syncs the instance first (a failure is a warning, and the game still starts), runs Java with the arguments untouched, prefixed by `settings.wrapper` when that is set, then records the run. A `settings.wrapper` that can't be run at all is a warning and the game starts with Java on its own. Without `--gameDir` it is the launcher's version check, which only runs Java. Java is `settings.java` when set, else `resolved.java`, shulker's managed runtime. The game's exit status is passed back as its own (`game-exit`).

Where no game started at all, `hook wrap` exits `launch-not-started`: the instance file couldn't be read, no Java is recorded, or the recorded Java wouldn't start. That exit is what makes the launcher raise an error, which is all a player sees when no window appears, and the last two also leave a `not-started` launch record, so `instances` and the launch history say the launch never happened. Shulker's own failures around a launch that is going ahead never turn into one: a failed sync and a wrapper that gave way both exit 0, because the game is starting either way. The arguments carry the session access token and appear in no output or record.

### `shulker watch`

The watcher a detached [`play`](#shulker-play) leaves behind, and not something to run by hand. It reads the launch from its stdin — never from its arguments, since the game's own arguments carry the session access token and a process list is public — starts the game, writes one line back with the game's process id, or with why it couldn't start it, then waits for the game to exit and records the run. It writes nothing else anywhere but the launch history. On Windows it runs with no console at all, and starts the game with none either, so neither opens a window.

## Types

`add`, `remove`, and `list` span every kind of thing a project requires. Each kind also has a group of its own, which is the plain verb with that `--type` and only the flags that kind takes.

### `shulker mod add|remove|list`

`shulker mod add sodium` is `shulker add sodium --type mod`, and the same for `remove` and `list`. Flags: `--side`, `--channel`, `--pin`, `--provider`, `--as`, `--with-deps`, `--skip-missing`, `--yes`, `--verbose`.

```sh
shulker mod add sodium
shulker mod list
```

### `shulker modpack add|remove|list`

A modpack is another shulker project whose mods and overrides merge into this one. `shulker modpack add ../base-pack` is `shulker add ../base-pack --type modpack`; the source is a local path, git URL, or raw manifest URL. A raw manifest URL brings only the modpack's `shulker.json` and `shulker.lock`, so adding, locking or updating one warns that its overrides and local files never arrive. `remove` prunes the mods only that modpack provided, and `list` shows each modpack's locked ref and whether a local one has changed. Flags: `--ref`, `--path`, `--as`, `--unlocked`, `--no-auto-update`, `--yes`, `--verbose`, and for a modpack from a provider `--pin`, `--channel`, `--provider`.

A modpack can be a Modrinth or CurseForge modpack, named by its slug: `shulker modpack add cozy` looks it up on each provider in the manifest's order, or on the one `--provider` names, and writes `{"type": "modpack", "provider", "project"}` under the slug unless `--as` says otherwise. It picks its version like a mod: the newest in its channel that fits the project's Minecraft and loader, or the newest overall when the project sets neither, in which case the project takes the pack's platform. None fitting fails with `no-compatible-version`. That version's archive is fetched into the cache and read as an archive is below, so its mods lock as the modpack's and its overrides are laid before your own; the lock records its provider, version and `sha512`. `pin`, `unpin`, `update` and `outdated` treat it as they treat a mod, and `sync` never moves it. A locked one builds offline from the cache; one not yet locked can't be fetched offline. `--ref`, `--path`, `--unlocked` and `--no-auto-update` are refused: the archive is a provider version, always locked, and moves only with `update`. An archive whose author turned off third-party downloads stops with `missing-files` until you put it in `downloads/`.

A modpack can also be a Modrinth modpack archive: `shulker modpack add packs/cozy.mrpack`, or a bare `shulker add packs/cozy.mrpack`, writes a `file` entry, taking the path the way `add` takes a local file — referenced where it lies inside the project, copied into `files/` from outside it, and keyed by the file name unless `--as` says otherwise. Its mods lock as the modpack's: each is found by its hash on Modrinth, or reused from the shulker project an exported archive carries, and a file neither knows, or one that project had as a local file, is laid by the modpack itself, as are its `overrides`, `client-overrides` and `server-overrides` folders, before your own. An archive is locked unless `--unlocked` says otherwise. The lock records its bytes, so changing the file makes the lock out of date and the next `lock` or `sync` reads it again, unless `--no-auto-update` holds it until `shulker update`. With the file deleted, the build lays the archive from the cache and warns. A CurseForge modpack zip is taken the same way, by `shulker modpack add packs/craft.zip` or by a bare `add` of a zip that holds a CurseForge `manifest.json`: each file it names locks as the modpack's by its CurseForge project and file ID, a file whose author doesn't allow third-party downloads stops the lock with `missing-files` until you put it in `downloads/`, where it locks as a manual download, and the pack's overrides folder is laid by the modpack. Those IDs carry no hash, so the cache can't stand in for CurseForge: reading the zip needs the network even when every file it names is cached, and offline it fails with `modpack-offline`. A locked zip whose bytes haven't changed builds from the lock and needs no network. A file that is neither a Modrinth nor a CurseForge modpack is refused.

A modpack that ships a `shulker.lock` is **locked**: its exact versions, dependencies included, are copied into this project's lock and marked with the modpack they came from, and its Minecraft and loader must match this project's exactly. A modpack without a lock, or one added with `--unlocked`, is **floating**: its mods are resolved here like your own, and its Minecraft and loader only have to admit this project's versions. A mod you list in `shulker.json` yourself always wins over either. Change your mind later with `shulker set requires.<key>.locked true|false`. On a terminal, adding a locked modpack built for another Minecraft asks `Unlock <name> and resolve its mods for Minecraft <version>?`, and Yes, or `--yes` anywhere, adds it as `--unlocked` would.

```sh
shulker modpack add https://github.com/shulker-sh/base-pack.git --ref v3
shulker modpack add ../base-pack --as base
shulker modpack add ~/Downloads/cozy.mrpack
shulker modpack list
shulker modpack remove base-pack
```

### `shulker resourcepack add|remove|list`

`shulker resourcepack add fresh-animations` is `shulker add fresh-animations --type resourcepack`, and the same for `remove` and `list`. The provider's own project type decides what an entry is, so the plain `shulker add` usually needs no `--type` at all. `add` records the provider's file name as the entry's `filename`, so the pack is placed as `resourcepacks/<that name>` under the name other packs' `options.txt` already enable, and it keeps that name when it updates, so a pack you enabled in game stays enabled. An entry with no `filename` is placed as `resourcepacks/<key>.zip`. Flags: `--channel`, `--pin`, `--provider`, `--as`, `--skip-missing`, `--verbose`.

```sh
shulker resourcepack add fresh-animations
shulker resourcepack list
```

### `shulker shader add|remove|list`

`shulker shader add complementary-reimagined` is `shulker add complementary-reimagined --type shader`, and the same for `remove` and `list`. A shader is placed under its entry's `filename`, which `add` sets to the provider's file name, or as `shaderpacks/<key>.zip` without one, and enabled through its shader mod's own config: `config/iris.properties`, or `config/oculus.properties` on Forge. One that ships vanilla core shaders needs no shader mod at all, so it is placed in `resourcepacks/` and enabled like a resource pack. Flags: `--channel`, `--pin`, `--provider`, `--as`, `--skip-missing`, `--verbose`.

```sh
shulker shader add complementary-reimagined
shulker shader list
```

### `shulker datapack add|remove|list`

`shulker datapack add terralith` is `shulker add terralith --type datapack`, and the same for `remove` and `list`. Modrinth files datapacks as mods, so a project whose only files are datapacks adds as one without `--type`, and one that ships both a mod and a datapack, like Terralith, adds as the mod unless `--type datapack` asks for its datapack files. A datapack is placed on both sides under its entry's `filename`, which `add` sets to the provider's file name, or as `<key>.zip` without one, in the folder of a global datapack mod the side places: `config/paxi/datapacks/` for Paxi, and `config/openloader/data/` before Minecraft 1.21 or `config/openloader/packs/` from it for Open Loader. With neither, a server places it in its world's `datapacks/` folder, named by `level-name`, which the game loads without a mod; a client places it in `datapacks/` and warns, since only some global datapack mods read that folder. `--side` narrows it to one side. A hybrid, a datapack that carries `assets/` as well, loads its assets only as a resource pack: `--resourcepack` records `"resourcepack": true`, which also places the same zip under the same name in the client's `resourcepacks/`, so one entry keeps both copies on one version. A local zip holding both `data/` and `assets/` needs `--type` or `--resourcepack`, which implies `--type datapack`. Load order isn't managed: ship Paxi's `datapack_load_order.json` as an override. Flags: `--side`, `--channel`, `--pin`, `--provider`, `--as`, `--resourcepack`, `--skip-missing`, `--verbose`.

```sh
shulker datapack add terralith
shulker datapack list
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

Show where the shared download cache is, how much space it uses, how many files it holds, how many of those are manual downloads, how many listing pairs the listing index holds, and how much `cache prune` would free. The roots line names what is keeping files: every instance in the registry, the project you are standing in when there is one, and each lock file `--lock` names, counted as `lock files`. A registered instance or named lock file whose lock can't be read is named as a warning and no prune line is suggested, since `cache prune` refuses while one is unreadable; the prunable figure is then counted as if that instance needed nothing.

```sh
shulker cache info
```

| Flag | Description |
| --- | --- |
| `--lock` | Also keep what this lock file references, as `cache prune --lock` would; repeat for more |

### `shulker cache verify`

Check the whole cache, since it belongs to the machine rather than a project. It rehashes every object against the sha512 it is stored under, asks Modrinth and CurseForge whether they still have each file a root locks, and lists the objects no root uses. The roots are the ones `cache prune` keeps: every registered instance, the project you run it in, the history entries each keeps, and each lock file `--lock` names. The takedown check sends one request per provider for all of them together, and names every root that locks a file gone from its provider, as it does for [`audit`](#shulker-audit). A provider that can't be asked is reported as skipped rather than passed, and a changed object is left out of it.

It exits non-zero on a changed object, a file gone from its provider, or a file its provider files under another project, ending with `cache-verify-failed`. A changed object means something rewrote the cache, and every build placing it would spread the change; `--fix` drops the changed objects, so the next build that needs one downloads it again, and a run whose only problem was a changed object it dropped succeeds. Unused objects are listed with their total size and don't fail it: [`cache prune`](#shulker-cache-prune) removes them.

```sh
shulker cache verify
shulker cache verify --fix
```

| Flag | Description |
| --- | --- |
| `--fix` | Drop the changed objects, so the next build downloads them again |
| `--lock` | Also check what this lock file references, as `cache prune --lock` keeps it; repeat for more |

With `--json`, `data` holds `objects` (how many were rehashed), `changed` (`sha512`, `size`) and whether `dropped`, `takedowns` and `moved` (each file's `provider`, `project`, `version`, `versionNumber`, `sha512` and `status`, `filedUnder` for a moved one, and the `keys` and `roots` that lock it), `skipped` (`provider`, `reason`), and `unused` (`sha512`, `size`) with `unusedBytes`. A failing run carries the same `data` under `cache-verify-failed`, whose `items` are the changed objects' hashes and the flagged files' keys.

### `shulker cache prune`

Remove everything in the cache that no root references. A root is a registered instance or the project you run it in: its `shulker.lock`, the lock of every history entry it keeps, the modpack checkouts and offline sync fallbacks its sources need, and the archive of a pack last imported from a file, which a re-import reads and no provider can fetch again. Manual downloads stay unless `--manual` is passed: a file you downloaded by hand is taken from the cache the next time any project needs it, and nothing can fetch it again. Installer logs and half-finished downloads always go, and so do listing index pairs no command has used for 90 days. The managed Java runtimes and your CurseForge key are never touched, and nothing a build placed can be removed from a directory without its bytes reaching the cache first, so rolling an instance back still works offline. A registered folder that no longer exists is skipped; one that is there but whose lock can't be read stops the prune, since it may be an instance that still needs its files. A detached build from `sync --into` is no root of its own: it runs on its source project's lock, which is kept while that project is a registered instance's source, a registered instance itself, or the project you run the prune in. Otherwise, and always for a detached build from a git or URL source, the prune may remove its files from the cache, and its next sync downloads them again; its own directory keeps them either way.

`--lock` adds a lock file as a root of its own, by path, whatever its name, a relative one taken from the directory you run shulker in rather than `--dir`, so a machine with no registered instances, like a CI runner caching several packs, can keep every pack's files in one prune: `shulker cache prune --lock a/shulker.lock --lock b/shulker.lock`. Only that lock is kept, not the history entries or sources of the project it came from. A named lock that isn't there fails with `lock-not-found`, and one that can't be read stops the prune like an unreadable instance.

```sh
shulker cache prune
```

| Flag | Description |
| --- | --- |
| `--lock` | Also keep what this lock file references; repeat for more |
| `--manual` | Also remove manual downloads, which nothing can fetch again |

### `shulker security`

Explain how shulker keeps bad files off your machine. It opens with what shulker aims for, lists what it does on every run and what each protection stops, and ends with a table of the settings that change a protection, once there are any. No project or source can turn off a protection that has no setting. Every security warning and error ends by pointing here, under `Read what shulker checks and why:`, and under `--json` a security error names its row in `error.protection`. With `--json`, `data` holds the `stance` and a `protections` row for each: its `id`, a one-sentence `summary`, whether it is `on`, and for a configurable one its `setting`, `value` and what it `changes`.

```sh
shulker security
shulker security --json
```

### `shulker log`

Show what shulker did, from `log.jsonl` beside `config.json`: when each run started and ended, and every warning and error it showed. It prints the last 24 hours by default, under a preamble that names the shulker version, the platform, the window and the filter that ran, and how many entries matched out of how many the log holds, so a slice pasted into an issue explains itself. An error is marked `✘` with its code, and its message goes on the line below. The report ends with the `--since` that widens it to every day the log keeps. A filter that matches nothing still prints the preamble, with `0 of <n> entries`, so it reads differently from a log that holds nothing. A log that isn't there is an empty report, and one that can't be read is a warning; neither fails the command. The filters combine, and `-i` takes an instance's id, name or directory, the way every other command does. A warning shown several times in one run is logged once, so the log isn't a count. A command that changes nothing, like `list`, `get` or `version`, is logged only when it warns or fails, and then without its result; one that writes a file or starts the game is logged every time, even with nothing to do. `shulker log` never logs itself. What it prints is redacted by default, in the themed view and under `--json` alike, so it is safe to paste in public: a URL loses the credentials before its `@` and keeps the rest, so `https://<token>@github.com/org/pack.git` reads `https://github.com/org/pack.git`; a CurseForge API key, yours or shulker's shared one, reads `[key]`; and your home directory reads `~`. The preamble ends with `redacted`, or `unredacted` under `--unredacted`, which prints the entries as `log.jsonl` stores them, for diagnosing your own machine. The file itself keeps everything, so only its owner can read it. The game's session access token is never written to it at all. Under `--json`, `data` holds the preamble's facts, `redacted`, and the matching entries. The log keeps the last [`log.keepDays`](#configuration) days, 30 unless it is set.

```sh
shulker log
shulker log --since 7d --level error
shulker log -i friends --group launchers
shulker log --cmd "hook wrap" --code launch-not-started
```

| Flag | Description |
| --- | --- |
| `--since` | Entries from this long ago (`24h`, `90m`, `7d`) or this date (`2026-09-01`) on. Default `24h` |
| `--group` | Only commands in this help group: `project`, `mods`, `builds`, `launchers`, `servers`, `play`, `shulker` |
| `--cmd` | Only this command and the ones under it, like `sync` or `"hook wrap"` |
| `--code` | Only entries with this error code |
| `--level` | Only entries at this level: `info`, `warn` or `error` |
| `--unredacted` | Print entries as stored, with the URL credentials, CurseForge keys and home directory the default hides |
| `-i, --instance` | Only entries about this instance, by id, name or directory |

### `shulker version`

Print the shulker version on one line, with when it was built: `shulker 0.0.1 (built 2026-09-20 14:02 UTC)`. A build from a clone prints `shulker dev` with its commit, and `-dirty` when the tree had uncommitted changes. `--verbose` adds a table: when it was built, the Go version and platform, how shulker was installed (`release`, `go install` or `source`, left out when it can't tell), and where the binary, `config.json` and the cache are. `--json` always carries every field.

```sh
shulker version
shulker version --verbose
```

| Flag | Description |
| --- | --- |
| `--verbose` | Also print the build and the environment |

### `shulker self update`

Replace the running shulker with the latest release from GitHub. It checks the download against the release's SHA256 checksums and, when the [GitHub CLI](https://cli.github.com) (`gh`) is installed, verifies its build provenance. Without `gh`, it installs on the checksum alone.

It only replaces a binary that came from a release archive, which is what the install scripts and a GitHub download give you. One installed with `go install` or built from a clone fails `self-update-unmanaged`, naming the command that updates it instead: `go install shulker.sh/shulker@latest`, or `go build .`. `--check` works for every build: it prints the latest release, and the command for this build's route. A build from a clone has no version to compare, so `--check` reports the latest release without saying whether it is newer, and `available` is `null` in JSON. `install` in the JSON names the route: `release`, `go install` or `source`.

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

A binary a package manager installed is left for it: the hooks come off and the registry is handled as above, then the last line names the command that removes the binary, `brew uninstall shulker` or `scoop uninstall shulker`. That is a success, not an error; in JSON, `removed` is empty beside `install`, which names the route the binary came from.

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
  "securityWarnings": [],
  "data": {}
}
```

| Field | Description |
| --- | --- |
| `ok` | `true` when the command succeeded |
| `command` | The command that ran, like `modpack add` |
| `lockStale` | `shulker.lock` doesn't match `shulker.json`; `shulker lock` brings it in line. Commands that build from the lock warn, naming each difference, and carry on; `export` refuses |
| `warnings` | Everything shulker would print as a `!` line without `--json`. Always present, empty when there are none |
| `securityWarnings` | The warnings a security protection raised, each also in `warnings` as its message: `protection`, the `id` of its row in [`shulker security --json`](#shulker-security), `message`, and `data` with its facts where it has any, like the versions [`security.minReleaseAge`](#configuration) held back. Always present, empty when there are none |
| `data` | The command's result, left out when it has none. When a command that works through several entries fails, like `sync --all`, it holds the result for each entry |
| `error` | Present when `ok` is `false`: `code`, `message`, and sometimes `help`, `candidates`, `items` or `protection` |

`help` says what to do about the error, like the command to run. `candidates` lists values you could pass instead, like the sides when a command is given something that is not one. `items` lists what the error is about, like the files in conflict. All three are left out when empty. `protection` is set only when a security protection refused something: it is the `id` of that protection's row in [`shulker security --json`](#shulker-security), so `mrpack-invalid` from a pack that tried to write outside its folder (`paths`) reads apart from one that is only malformed.

| Exit status | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Failure; `error.code` says which |
| `2` | Usage: an unknown command or flag, wrong arguments, or a flag value that isn't allowed |
| `130` | Interrupted (`interrupted`) |

`serve` exits with the server's own status when the server fails (`server-exit`).

`completion <shell>` is the one exception: it prints its script, since a shell sources it as it is.

### Help

`--help`, `help [command]`, and a command that groups others run without a subcommand, like `shulker feature`, return the command's help as `data`:

| Field | Description |
| --- | --- |
| `command` | The command the help is for, like `feature list`; empty for shulker itself |
| `short` | Its one-line summary |
| `description` | Its description, one string per paragraph |
| `usage` | Its usage line |
| `commands` | Its subcommands, each with `name` and `short` |
| `flags` | Its own flags, each with `name` and `usage`, plus `shorthand`, `type` (the value it takes, like `string`; left out for a switch) and `default` when it has them |
| `globalFlags` | The flags every command takes, in the same shape |
| `examples` | Example command lines |
| `docs` | The command's page on this site |

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

Without `--json`, the error line ends with its code, like `✘ sodium is not in the manifest (mod-not-found)`, with the items and candidates in a tree underneath.

| Code | Meaning |
| --- | --- |
| `account-exists` | An account already answers to that name, or already plays under that UUID. A name is shared with `--force`; a UUID never is |
| `account-name-invalid` | An offline name outside the pattern a Minecraft username matches; `--allow-invalid-name` takes it |
| `account-not-found` | No account matches the selector, the account an instance is pinned to has since been removed, or `config set accounts.default` names an id no account has. `candidates`: every account, each as its qualifier and its id, `pass`: their ids |
| `account-not-playable` | The account owns no Java profile, so it can't launch anything |
| `account-sign-in-expired` | The Microsoft refresh token is gone or revoked, so shulker can't get a session for the account; `shulker accounts login` signs it in again |
| `accounts-invalid` | shulker's own `accounts.json` isn't valid JSON (the message names the line and column), names a `$schema` this shulker doesn't know or names none, or doesn't match its schema |
| `already-ignored` | The pair already has an ignore in `shulker.json`; pass `--force` to replace it |
| `ambiguous-account` | The selector can't tell several accounts apart, since they share the very same name or their ids share the prefix typed, and shulker can't ask, because it isn't running on a terminal. `candidates`: the matches, each as its qualifier and its id, `pass`: their ids |
| `ambiguous-instance` | Several instances match the name given, or several shulker instances play the project `play` was run in. `candidates`: the matches, `pass`: their ids, which are unique |
| `ambiguous-into` | The side has edits in several synced directories; pass `--into`. `candidates`: the directories |
| `ambiguous-side` | The manifest declares both sides and the command works on one; `sync` and `pull` take `--side`, `diff --into` names it. `candidates`: the sides |
| `appdata-unset` | `APPDATA` isn't set on Windows, so shulker can't find a launcher's default folder. `link` takes `--launcher-dir` instead |
| `archive-not-modpack` | The file given as a modpack isn't one: not a zip, or a zip without the index its format needs, like a CurseForge `manifest.json` of type `minecraftModpack`. A modpack's `file` that is neither a Modrinth nor a CurseForge modpack is refused the same way |
| `backup-failed` | `backup --all` failed for some targets; `data` has each target's result |
| `backup-invalid` | `restore` was given a zip that won't open, or that holds anything other than world folders at its root |
| `backup-missing` | `restore` found no backup with that number, or none by the name or at the path `--backup` gives |
| `backups-empty` | `restore` found no backups for the target |
| `build-conflict` | Files changed both in the build directory and in the source; run `diff`, or pass `--force` to overwrite, which also resets seeded files. A sync for a launch keeps them instead, and a seeded file never conflicts. `items`: the files |
| `build-reserved` | A side that builds in place has overrides that would write `shulker.json`, `shulker.lock`, `shulker.local.json`, `.shulker/` or a data directory. `items`: the files |
| `cache-changed` | A cached file no longer matched its hash, so the build deleted it, and it has no URL to download it from again: a local file or a manual download. `shulker install` puts the locked copy back |
| `cache-verify-failed` | `cache verify` found a changed object it didn't drop, a file gone from its provider, or one its provider files under another project. `items`: the changed objects' hashes and the flagged files' keys; `data`: the whole report |
| `cache-root-unreadable` | A registered instance's `shulker.lock`, or a lock file named with `--lock`, is there but can't be read, so `cache prune` stops rather than remove files it may need; `cache info` still reports and names it |
| `audit-failed` | `audit` found locked files gone from their provider, or entries that download from outside it or that it files under another project. `items`: their keys; `data`: the whole report |
| `check-failed` | `check` found a problem; each one printed above it. `items`: every problem's items as `<code>: <item>`; `data.problems`: each problem as an error |
| `checksum-mismatch` | A download's hash isn't the one recorded for it: the sha512 in the lock or from the provider, or the sha1 in a version JSON or Java runtime manifest. Rows show both hashes, and the file at `install` |
| `class-file` | `audit file` was given a class file, which `audit class` reads instead |
| `config-dir-unset` | The OS can't say where this user's config or data folder is, usually because `HOME` isn't set. Set `SHULKER_CONFIG` and `SHULKER_DATA` instead |
| `config-invalid` | shulker's `config.json` isn't valid JSON (the message names the line and column), or names a `$schema` this shulker doesn't know or names none. Only commands that need its registry location fail, as they do when it fails `schema-newer`; the rest warn and go on without it. `shulker config set` replaces it, keeping the old file as `config.json.replaced` |
| `curseforge-cant-place` | `export curseforge` can't name these datapacks by file ID, since the CurseForge app installs datapacks in `datapacks/` and the build places them elsewhere: in a global datapack mod's folder, or a hybrid's copy in `resourcepacks/`; pass `--bundle`. `items`: each datapack and its folder |
| `curseforge-invalid` | The CurseForge modpack is malformed: its `manifest.json` doesn't parse, names no Minecraft version, is a manifest version other than 1, or the zip holds an unsafe path |
| `curseforge-key-rejected` | CurseForge rejected the API key: your own, or shulker's built-in one when shulker.sh has no working replacement |
| `curseforge-not-found` | `export curseforge` found nothing on CurseForge for these mods, resource packs, shaders or datapacks; pass `--bundle`. `items`: what is missing |
| `dependency-overrides-invalid` | Fabric Loader would refuse the `config/fabric_loader_dependencies.json` a side's build places, so the game wouldn't start: its first key isn't `"version": 1`, a key or dependency kind is unknown, or a range isn't a string or array of strings. The `cause` row says which |
| `deps-held` | A mod being added needs another version of a dependency the lock holds; `--with-deps` moves them. `items`: each held version and what needs it |
| `download-failed` | A provider failed to serve a file at `install`, `sync`, `serve`, `export` or `check`, or while `add`, `lock` or `import` locked it: its CDN cut the file short, answered with an HTTP error, or the connection dropped (one that drops before the server answers is retried twice first). The message names the file and provider; rows show the URL and cause. Help says to try again later, or to run `shulker update` when the provider no longer has the file. Every file is tried before the run fails, unless `--fail-fast`: with several failures the message counts them, a row names each file with its URL and cause, and `items` holds each file's message. When files also need a manual download, `missing-files` is the run's error and this one is in `data.errors` |
| `dump-failed` | On Windows, `jcmd` ran but took no thread dump of the game, so the pid is no JVM or one it may not attach to. The row holds what `jcmd` said |
| `dump-timeout` | The game `instance dump` asked for a thread dump printed none within 10 seconds, which a JVM started with `-Xrs` never does |
| `editor-failed` | The editor `instance edit` ran couldn't be started or exited with an error; set `$EDITOR` to the one you use |
| `error` | Anything unexpected, like a file that can't be read or written. The message has the details |
| `eula-required` | The server needs the Minecraft EULA accepted |
| `feature-not-found` | No mod or feature declaration uses the feature. `candidates`: the features in use |
| `file-not-found` | A file named to `pull` isn't in the build directory, a path given to `add` or `match` isn't a file, a path given to `import` isn't there, or a mod or modpack's `file` in `shulker.json` names a folder. Also a path given to an `audit` inspection command that isn't a file, and a path inside a jar that the jar doesn't hold. `candidates`: the closest file there, for `pull` |
| `file-taken` | `add` would copy a local file or folder into `files/`, which already holds a different one of that name that no entry of the same key names; rename one or remove the one in `files/`. Also an `import` whose pack names two different local files of one name |
| `game-exit` | The game `hook wrap` ran exited with an error; the exit status is the game's own |
| `game-not-running` | `instance dump` found no game running in the instance: no run is open, its game has gone, or another launcher started it, so shulker has no process to ask; or no process has the pid `--pid` names |
| `game-wrapped` | `instance dump` won't signal a game started through `settings.wrapper`, since the pid it knows is the wrapper's. The row names that pid; pass Java's own with `--pid` |
| `git-missing` | A git source needs `git` on PATH |
| `group-not-found` | `--group` names a save group that isn't under the saves root |
| `history-empty` | The instance has no history entries yet; one is taken before an in-place build changes anything |
| `history-invalid` | A history entry's own record or its lock is unreadable; the message names the entry to delete |
| `history-missing` | There is no history entry with that number; `shulker history` lists the ones kept |
| `import-into-self` | `import` was given the project's own folder |
| `import-mismatch` | `import` into a project whose Minecraft version, loader or loader version, set or inherited, differs from the pack's; import it into a new folder with `-C` |
| `installer-failed` | NeoForge's or Forge's own installer failed while setting up a server dir or a launcher; the message shows its last output and names the log in shulker's cache that holds all of it |
| `instance-exists` | An instance already follows a different modpack, or is an ATLauncher or GDLauncher instance shulker didn't link; pass `--name` (`--as` for `link shulker`) for a second one, or `--force` |
| `instance-id-taken` | Another instance already has the `--as` id; the message names its directory |
| `instance-invalid` | An instance's `.shulker/instance.json` isn't valid JSON (the message names the line and column), names a `$schema` this shulker doesn't know or names none, or doesn't match its schema; `shulker instances repair` writes it again, keeping the old file as `.shulker/instance.json.replaced` |
| `instance-missing` | A linked instance's directory is gone |
| `instance-not-found` | No instance matches, or the directory `shulker instance` acts on holds no `.shulker/instance.json`. `candidates`: the instances shulker knows, `pass`: their ids |
| `interrupted` | Ctrl-C or SIGTERM stopped the command. Files are left whole: each one is written in full or not at all. A second Ctrl-C quits at once |
| `into-missing` | The `--into` directory does not exist |
| `into-required` | Syncing from a remote source needs `--into` |
| `jar-invalid` | A jar given to an `audit` inspection command, or one nested in it, isn't a readable zip, or a file in it inflates past 512 MiB |
| `jar-metadata-invalid` | A mod jar's metadata (`fabric.mod.json`, `quilt.mod.json` or `mods.toml`) can't be read, or the jar isn't a readable zip. The row names the file and what went wrong |
| `jar-metadata-missing` | A mod jar has none of the metadata files the loader reads |
| `java-not-found` | No working Java at the configured path or on PATH |
| `java-range` | `java` in `shulker.json` is neither a path nor a version range |
| `java-version` | The Java found is outside the range in `shulker.json` |
| `jcmd-not-found` | On Windows, the game's runtime has no `jcmd` beside its `java` for `instance dump` to take a thread dump with. The row names the path it looked for |
| `jvm-flags` | Unknown `jvmFlags` preset |
| `key-not-found` | A `--key` isn't in the file. `candidates`: its keys |
| `launch-not-started` | Shulker never got as far as running the game: for `hook wrap`, the instance file couldn't be read, no Java is recorded, or the recorded Java wouldn't start; for `play`, the Java it assembled wouldn't start, or the watcher it hands a detached launch to couldn't be started or stopped before it answered. Under a launcher the exit is what makes it show an error, since no window appears |
| `launcher-account` | The account belongs to another launcher, so only that launcher can sign it out, renew it or remove it |
| `launcher-dir-required` | MultiMC needs `--launcher-dir` |
| `launcher-file-invalid` | A launcher file shulker reads or rewrites (an instance's JSON, `launcher_profiles.json`, `mmc-pack.json`) isn't valid JSON, or not the shape shulker expects. A row carries the parser's own error |
| `launcher-not-found` | No launcher directory where shulker looked |
| `loader-install-incomplete` | The loader's installer left no launcher profile to read the installed version from |
| `loader-profile-invalid` | The loader profile shulker fetched isn't a version JSON with an id, so it can't be installed into the launcher. A row says what was wrong with it |
| `loader-required` | `add` of a mod in a project without a loader, or `import` of a pack that names mods but no loader; set one with `shulker set loader.type <loader>`. On a terminal `add` asks `Which mod loader?` instead, sets `loader.type` to the answer and carries on |
| `loader-version-unsupported` | The locked Forge version ships the legacy installer, which shulker can't run: every Forge before Minecraft 1.12.2, and 1.12.2 builds before 14.23.5.2851 |
| `local-file` | `pin`, `unpin` or `lock <key>` named a local `file` entry, which has no provider version to pin or look up |
| `local-file-missing` | A local `file` entry's file is gone and the cache has no copy of the bytes it was locked at, at `lock`, `sync` or any command that relocks; put the file back or remove the entry. A modpack's `file` entry resolves in the modpack's own directory, so one its author never committed fails the same way, and a modpack archive that is gone fails the same way too. While the cache still has them, a gone file only warns and builds from the cache |
| `local-invalid` | `shulker.local.json` isn't valid JSON, or names a `$schema` this shulker doesn't know or names none. It never fails a command: the file is moved aside to `shulker.local.json.replaced` with a warning, and the manifest's feature defaults apply |
| `lock-invalid` | `shulker.lock` isn't valid JSON (the message names the line and column), names a `$schema` this shulker doesn't know or names none, or doesn't match its schema (one line per failing field, by dotted path), or a change would make it invalid. `shulker lock` replaces it, keeping the old file as `shulker.lock.replaced`. `items`: the failing fields when there are several |
| `lock-not-found` | No `shulker.lock`; run `shulker lock`. Also a lock file named with `--lock` to `cache info` or `cache prune` that isn't there |
| `lock-stale` | `export` and `check` need a lock that matches `shulker.json`; run `shulker lock`. Other commands only warn. `items`: each difference |
| `manifest-exists` | A `shulker.json` is already where `init` would write one |
| `manifest-invalid` | `shulker.json` isn't valid JSON (the message names the line and column), names a `$schema` this shulker doesn't know, or doesn't match its schema (one line per failing field, by dotted path), or a change would make it invalid. A manifest with no `$schema` is read as the current version, and shulker writes the line the next time it saves the file. `items`: the failing fields when there are several |
| `manifest-not-found` | No `shulker.json` in the project directory or the sync source |
| `manual-download` | The provider doesn't distribute this mod or modpack; download it into `downloads/`. `add` waits for it at a terminal first |
| `memory` | Server memory isn't a whole number of M or G |
| `meta-fetch` | Version metadata couldn't be read from Mojang, a loader's meta or Maven, or GDLauncher's meta. The row names the service and what went wrong |
| `meta-invalid` | Version metadata was read but lacks what shulker needs, like a Minecraft version Mojang doesn't list, a Java runtime manifest with no java in it, or a loader installer whose files won't parse |
| `microsoft-account` | `accounts remove` was given a Microsoft account, which is signed out with `accounts logout` rather than deleted |
| `minecraft-required` | `shulker.json` sets no `minecraft` and no locked modpack supplies one; set it with `shulker set minecraft <version>` |
| `missing-files` | Files that need a manual download are missing, at `install`, or files a CurseForge modpack names at `import` (both wait for them at a terminal instead, as `install` does for a hosted modpack's archive) or when a modpack's CurseForge zip is read, or a hosted modpack's archive when its author turned off third-party downloads; also a local `file` entry whose file is gone or changed when the cache has no copy either. Only `.jar` and `.zip` files in `downloads/` are read, since Minecraft keeps its own files in an instance's `downloads/`. `items`: what to download or restore |
| `mod-not-found` | The mod isn't on any provider, or isn't in `shulker.json`. `candidates`: the mods in `shulker.json`, where relevant |
| `modpack-changed` | A modpack no longer matches the lock; run `shulker update`. Also a locked modpack whose local `file` has changed since the modpack was locked, when the cache has no copy of the locked bytes; run `shulker lock` in the modpack |
| `modpack-conflict` | Two modpacks list the same mod with different settings |
| `modpack-download` | A file a modpack archive lists couldn't be downloaded |
| `modpack-exists` | The modpack is already in `shulker.json` |
| `modpack-fetch` | A modpack couldn't be fetched |
| `modpack-lock-missing` | A modpack is set `locked: true` but its source has no `shulker.lock`; run `shulker lock` there, or set locked false |
| `modpack-lookup` | A file a modpack archive lists, or one in its override folders, couldn't be looked up on Modrinth or CurseForge, by `import`, `match`, or when a modpack archive is read. The `modrinth` or `curseforge` row says why; offline, it is the network the lookup needs |
| `modpack-manifest` | A modpack source has no `shulker.json` |
| `modpack-mismatch` | A modpack wants a different Minecraft version or loader |
| `modpack-not-found` | The modpack isn't in `shulker.json`. `candidates`: the modpacks |
| `modpack-offline` | A modpack archive that lists its files by provider ID, like a CurseForge zip, was read without the network. An ID carries no hash, so no cached copy can stand in; the provider's row, when there is one, is the network error |
| `modpack-path` | A modpack's `path` is set on a source that isn't git |
| `modpack-platform` | Locked modpacks disagree about Minecraft or the loader, and `shulker.json` sets neither; set `minecraft`/`loader`, or unlock one |
| `modpack-provided` | The mod comes from a modpack, so it can't be removed on its own, or looked up again with `lock <key>`: lock a hosted modpack again with `lock <modpack>`, and a git, URL or folder modpack's author has to fix its lock |
| `modpack-ref` | A modpack's `ref` doesn't apply to its source, or wasn't found |
| `modpack-unlocked` | A modpack has no commit, archive hash or version in the lock; run `shulker update`, or `shulker lock` before pinning a hosted one |
| `modpack-url-file` | A modpack fetched from a URL has a local `file` entry; a bare manifest carries no files, so serve the modpack from git or a directory |
| `mrpack-host-not-allowed` | Modrinth launchers only download over https from `cdn.modrinth.com`, `github.com`, `raw.githubusercontent.com` and `gitlab.com`, so they won't download these files, and a local `file` entry has no download at all; pass `--bundle`. `items`: the files |
| `mrpack-invalid` | The modpack is malformed; its index names a path outside the pack's folder (a `..` component, a leading `/` or `\`, a drive letter, or a Windows device name like `CON` or `NUL`); or a file downloads from anywhere but https on `cdn.modrinth.com`, `github.com`, `raw.githubusercontent.com` or `gitlab.com`, the hosts a Modrinth launcher allows |
| `mrpack-marker` | The modpack's own `shulker.json`, `shulker.lock` or `shulker.overrides.json` can't be read, whether it came from the archive root or the marker jar |
| `mrpack-unsupported` | The modpack's format isn't supported |
| `no-accounts` | shulker can see no account at all, so there is nothing to play with |
| `no-compatible-version` | The mod has no version for this Minecraft and loader. `candidates`: other release channels that have one. The example passes `--channel` to `add` and its type aliases, the commands that take it; anywhere else, such as `lock`, `pin` or `update`, it sets the entry's channel: `shulker set requires.<key>.channel beta` |
| `no-instances` | Nothing is linked yet |
| `no-problem` | The locked mods have no dependency problem for the pair; pass `--rule` and `--declared` from the failed command. `candidates`: the current problems, where there are any |
| `no-side` | `shulker.json` declares no side of the kind the command needs. A local command (`build`, `diff`, `serve`) says to add the block; a command that can take a remote source (`sync`, `export *`) says to pass `--assume-client` |
| `no-worlds` | `backup` found no worlds to zip; the message names the folder it looked in |
| `not-built` | The side has no build directory yet; run `shulker build` |
| `not-direct` | The mod is only a dependency. `items`: the mods that require it |
| `not-drifted` | A file named to `pull` has no changes. `candidates`: the changed files |
| `not-ignored` | The pair has no ignore in `shulker.json`. `candidates`: the pairs that do |
| `not-in-place` | The project has no side that builds in place, so it keeps no history |
| `not-installed` | A file isn't in the cache; run `shulker install` |
| `not-on-provider` | `lock <key>` named a modpack from a git, URL or folder source, which has no provider version to look up |
| `not-pinned` | The mod has no pin |
| `not-shulker` | The instance belongs to another launcher, which starts it itself |
| `not-synced` | The directory has no record of the source it was synced from, or `audit -i` found no lock from the instance's last sync |
| `offline-account` | `accounts logout` or `accounts refresh` was given an offline account, which has no sign-in; `accounts remove` deletes it |
| `override-path` | A path named to `match` isn't a jar in `mods/` or a zip in `resourcepacks/`, `shaderpacks/` or a datapack folder of `overrides/`, `client-overrides/` or `server-overrides/` |
| `overrides-invalid` | The `shulker.overrides.json` at a modpack archive's root, where an export records which folder each override came from, isn't valid; it reaches you as `mrpack-marker` |
| `ownership-unproven` | Shulker can see no account that owns Minecraft: Java Edition, so it won't create an offline account — or delete one, since the same gate would block creating it again; `--force` deletes it anyway |
| `pack-filename-taken` | Two resource packs or shaders would be placed under one file name in the same folder, compared without case. Give one a different `filename` |
| `pack-unknown` | `client.resourcePacks` or `client.shader` names a pack the lock doesn't have, as a pack of a modpack in `requires` can be. Fix the name, or add the pack first |
| `path-invalid` | `shulker.json`, `config.json` or an instance's settings have no such field, or the path goes inside a single value or a list. `candidates`: the fields allowed there |
| `path-not-set` | `get`, `config get` or `instance get` names a field that isn't set |
| `path-outside` | A path a lock, manifest or modpack archive names would leave its folder: a local file, an override, or a git modpack's `path`. The schemas and the archive readers refuse such a path first, so this is a second guard |
| `path-taken` | `import` built the new project, but the folder it goes into has a file where the project has a folder, or the other way round; nothing was moved into it |
| `pin-mismatch` | The pinned version belongs to a different project |
| `platform-not-found` | No published Minecraft or loader version matches the manifest's `minecraft` or `loader.version` range |
| `player-invalid` | Neither a player name nor a uuid |
| `player-reassigned` | Player names now belong to different accounts; pass `--accept-player-change`. `items`: the players |
| `player-unknown` | Players that don't exist at Mojang. `items`: the names |
| `player-unresolved` | A player isn't in the lock; run `shulker player` |
| `players-invalid` | A player entry in `shulker.json` is invalid |
| `properties-invalid` | `server.properties` keys removed in this Minecraft version, or values that aren't valid, including a `shulker.json` value that can't be written as a property. Unknown keys only warn, with a did-you-mean. `items`: the problems |
| `provenance-mismatch` | A lock entry names a provider but downloads from a host that isn't one of that provider's, at `build`, `install`, `sync`, `export`, `check files` or a launch. `items`: the entries. Its help says to run `lock <key>` to look each up again, or, for an instance synced from a git or URL source, that the lock is the source's and its author has to fix it; a launch's sync that fails this way leaves the last good build in place and the game starts on it. `lock`, `update`, `pin`, `unpin` and `remove` still accept such a lock, since they're how it gets fixed |
| `provider-unavailable` | The provider isn't set up, like CurseForge without an API key |
| `rate-limited` | Modrinth or CurseForge is refusing shulker's requests for making too many; CurseForge refusing a key it has already accepted in the same run counts too. A Modrinth limit that resets within a minute is waited out once first; the help says when to run the command again |
| `registry-has-instances` | `config set` or `config unset` would move the registry away from instances the new one doesn't have; `--force` changes it anyway. `items`: the directories left behind |
| `registry-invalid` | shulker's `registry.json`, the list of linked instances and synced directories, isn't valid JSON (the message names the line and column), names a `$schema` this shulker doesn't know or names none, or doesn't match its schema; `shulker instances repair` rebuilds it, keeping the old file as `registry.json.replaced` |
| `release-too-new` | Every version of the mod, or of a dependency, on its channel was published more recently than [`security.minReleaseAge`](#configuration), so none can be chosen yet. The message says how old the newest is; the help says the day it qualifies and the `add --pin` that takes it now. `protection`: `release-age` |
| `requires-taken` | Another `requires` entry already holds the key, or the mod's jar id is already locked under another key; pass `--as <key>` |
| `requires-unsupported` | A `requires` entry or a project being added is a kind shulker can't resolve |
| `resourcepack-conflict` | `server.resourcePack` pushes a pack while `resource-pack` or `resource-pack-sha1` is also set in `server.properties` |
| `resourcepack-local-file` | `server.resourcePack` names a local `file` entry, which has no URL for clients to download it from |
| `resourcepack-not-distributed` | `server.resourcePack` names a pack its provider forbids redistributing, so the lock has no URL for it |
| `resourcepack-not-found` | `server.resourcePack` isn't a locked resource pack. `candidates`: the locked resource packs |
| `restore-failed` | `restore --all` failed for some targets; `data` has each target's result |
| `rosetta-required` | On an Apple Silicon Mac, the locked Java runtime, like Java 8's `jre-legacy`, is published only for Intel Macs, and Rosetta isn't installed to run it. The first `Fix:` row installs it; the second is the side's own, as for `runtime-unavailable` |
| `run-not-found` | `instance log` found no run to print: the instance has never been played, or the latest run's log has been deleted |
| `runtime-unavailable` | Mojang publishes no Java runtime for this platform, and on an Apple Silicon Mac no Intel one either. The `Fix:` row depends on the side: a server sets `java` in `shulker.json`, a client instance passes `--java <path>` to `shulker link` |
| `saves-failed` | `saves --all` or `saves prune --all` failed for some targets; `data` has each target's result |
| `schema-newer` | `shulker.json`, `shulker.lock`, `.shulker/instance.json`, `registry.json`, `config.json` or `accounts.json` was written by a newer shulker, and this one can't read it; the message names both schema versions, and `shulker self update` catches up. A newer `shulker.local.json` or `.shulker/state.json` warns instead, with the same fix |
| `self-uninstall` | The shulker binary couldn't be removed |
| `self-update-check` | Checking for a release failed, or none is published for an update (`--check` says so and exits 0) |
| `self-update-checksum` | The download doesn't match its checksum |
| `self-update-download` | The download failed |
| `self-update-install` | The running binary couldn't be replaced |
| `self-update-provenance` | `--require-attestation` is set and the build provenance couldn't be verified |
| `self-update-unmanaged` | The running binary isn't from a release archive, so shulker can't replace it; the message names what installed it and the command that updates it |
| `server-exit` | The server exited with an error. `items`: its `logs/latest.log` and, when the server wrote one during the run, its crash report; `data` carries them as `log` and `crashReport` |
| `shim-build-failed` | On Windows, shulker couldn't make an instance's `javaw.exe` shim from its own binary, because that binary isn't a Windows executable it can patch |
| `sign-in-failed` | The Microsoft sign-in didn't finish: it was declined, the code ran out before it was used, or Microsoft or Xbox Live refused it — including an account with no Xbox profile, which can't reach Minecraft at all |
| `source-fetch` | The sync source couldn't be fetched |
| `source-incomplete` | The sync source is a raw manifest URL whose manifest has a local `file` entry, which can't come with it; use the repository's git URL |
| `source-lock` | The sync source has no `shulker.lock` |
| `source-offline` | Offline, and the source has never synced here, so there's no copy to use |
| `source-path` | `--path` doesn't apply to the source, isn't a folder inside the repository, or holds no shulker.json at the commit |
| `source-ref` | `--ref` doesn't apply to the source, or wasn't found |
| `source-unknown` | `sync --into` found no record in the directory of what it was synced from; name the source |
| `state-invalid` | An instance's `.shulker/state.json` isn't valid JSON, or names a `$schema` this shulker doesn't know or names none. It never fails a command: the build warns and treats every file in the directory as not written by shulker |
| `store-incomplete` | The game store can't supply what a launch needs: a file with no source that isn't on disk, a native jar that won't unpack, or a version JSON that doesn't hold together |
| `strict-warnings` | `check --strict` saw warnings. `items`: the warnings |
| `sync-failed` | Some entries failed to sync; `data` has each entry's result |
| `topic-not-found` | `docs` found no page, heading or line matching the words. `candidates`: the pages |
| `type-ambiguous` | A CurseForge slug matches projects of several types, or a zip or folder given to `add` holds no pack whose kind it can tell, including one with both `data/` and `assets/`; pass `--type` to choose. `candidates`: the types it could be |
| `type-mismatch` | `--type`, or a hosted modpack entry, disagrees with what the provider says the project is. `candidates`: the provider's own type |
| `unlink-failed` | Some entries couldn't be unlinked; `data` has each entry's result |
| `unset-variable` | A `.tmpl` override or a `server.properties` or `client.options` value uses a variable that isn't set |
| `unsupported-loader` | shulker doesn't support the loader yet |
| `unsupported-quickplay` | `play --world` on a Minecraft before 1.20, which has no quick play to boot into a save with; nothing is launched |
| `update-paused` | The pre-launch hook stopped a GDLauncher update at four minutes so it could explain itself; the launch is aborted, and launching again resumes it. Shown in GDLauncher's own dialog, so it prints without shulker's usual error decoration |
| `url-insecure` | A download, API request or redirect named a plain `http://` URL, or a git source is on `http://` or `git://`. Shulker fetches over https only and clones over https, ssh or a local path, so a lock, modpack archive or source on a plain URL fails until its author changes it |
| `usage` | An unknown command or flag, wrong arguments, or a flag value that isn't allowed. The human error folds the command's usage line and its `--help` into its tree as `usage:` and `help:` rows. `items`: the missing or unexpected arguments, when that's the problem. Exits 2 |
| `validation-failed` | The locked mods have dependency problems, checked for each side against the mods its build places; each prints the `shulker ignore` command that would accept it, and a problem only some sides have names them. `items`: the problems |
| `version-no-file` | The provider's version has no file shulker can download, or no hash to check it against |
| `version-not-found` | The provider has no version with the id given to `add --pin` or `pin`, or the one a Modrinth or CurseForge URL names, or no file with an id a CurseForge modpack names; for a pin its help links the project's versions page. At `lock <key>`, the provider no longer has the locked version, and its help says to run `update <key>` |
| `version-required` | `export mrpack` and `export curseforge` need a version |
| `world-in-use` | `restore` would replace a world a running game or server has open. `items`: the open worlds |
| `world-not-found` | `backup --world` named a world the target doesn't hold, `restore --world` one the zip doesn't hold, or `restore` into a server was given a zip without the world its `level-name` names and no `--as`; the message names the `level-name` |
