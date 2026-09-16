---
description: How manifests, locks, targets, sides, overrides, build edits, modpacks, and providers fit together.
---

# Concepts

## Manifest

`shulker.json` declares the Minecraft version, loader, targets, mods, and modpacks. See the [manifest reference](/docs/manifest).

## Lock

`shulker.lock` records the exact resolved versions, hashes, and modpack commits. See the [lock reference](/docs/lock).

## Targets

A target is a build output: a client instance or a server directory. Each target has its own build directory and override layers, so one project can produce a client and a server from the same mod list. Manage them with [`shulker target`](/docs/cli#shulker-target-add).

## Sides

Every mod has a side: `client`, `server`, or `both`. Shulker reads it from the provider, and you can override it with `side` on the mod. A target only gets mods for its own side plus those marked `both`, so client-only mods like Iris never end up on a server. Resource packs and shaders are always client-only, and a server build leaves them out.

## Overrides

Overrides are folders of files copied into a build on top of the mods: configs, resource packs, scripts. A target lists its layers in order and later layers win, so a shared `overrides/common` can sit under a `overrides/server`. Files ending in `.tmpl` have `${name}` replaced with the target's [`variables`](/docs/manifest#variables) and are written without the suffix.

Some files are generated instead of copied: `server.properties`, `options.txt`, and the whitelist, ops, and ban lists from `shulker.json`, and a shader mod's `iris.properties` from the shader you locked.

## Edits in the build directory

Each build records what it wrote in `.shulker/state.json` in the build directory, so the next build can tell your edits apart from its own files. That way config you change in-game survives:

- A file you edited is kept, as long as its source hasn't changed.
- If the file changed in both places, or a file shulker didn't write is in the way, the build stops and lists it. Use `shulker build --force` to overwrite.
- Generated files like `server.properties` merge per key, so a key you edited in-game and a key you changed in `shulker.json` both apply. If both changed the same key, `shulker.json` wins and the build warns.
- A `.properties` file in your overrides merges per key too. shulker manages only the keys it lists, and leaves any others a mod writes alone. A file the mod wrote first gets those keys merged in instead of stopping the build. List a path in the target's [`wholeFiles`](/docs/manifest) to copy it whole instead.

Use [`shulker diff`](/docs/cli#shulker-diff) to see what changed, and [`shulker pull`](/docs/cli#shulker-pull) to copy those edits back into your overrides or `shulker.json` so they're part of the project.

## Modpacks

A modpack is another shulker project whose mods and overrides merge into this one. Its overrides sit beneath your own layers. A modpack that ships a `shulker.lock` is **locked**: the exact versions it pins, dependencies included, are copied into your lock and marked with the modpack they came from, and its Minecraft and loader have to match yours exactly — or, when you set neither, yours are taken from it, and every locked modpack has to agree. One with no lock, or added with `--unlocked`, is **floating**: its mods are resolved here alongside your own, and its versions only have to fit your ranges. A mod you list in `shulker.json` yourself always wins over either. Add one from a local path, git URL, or manifest URL with [`shulker modpack add`](/docs/cli#shulker-modpack-add-remove-list).

## Resource packs and shaders

Resource packs and shaders sit in `requires` beside your mods, and the provider's own project type decides which is which, so `shulker add fresh-animations` needs no `--type`.

Each is placed under its `requires` key — `resourcepacks/<key>.zip` and `shaderpacks/<key>.zip` — rather than the provider's file name, which changes with every version. Minecraft and Iris both enable packs by literal file name, so a stable name is what keeps a pack you turned on from quietly switching off the next time it updates.

A shader is enabled through its shader mod's own config, `config/iris.properties`, or `config/oculus.properties` on Forge. Shulker writes only `shaderPack` and `enableShaders` there, so the rest of your shader settings survive a rebuild. A shader that ships vanilla core shaders needs no shader mod at all: it goes in `resourcepacks/` and is enabled like a resource pack.

The enabled list in `options.txt` works differently, because it is in priority order and yours to arrange. Shulker seeds it once — on the first build, when the line is missing or still Minecraft's own `["vanilla"]` — and then leaves it alone. A pack you add later is placed but not enabled: turn it on in game, and shulker won't reorder what you chose. `shulker build --force` seeds the list again.

## Providers

Mods, resource packs and shaders are resolved from Modrinth or CurseForge. By default Modrinth is tried first, then CurseForge. Set [`providers`](/docs/manifest#properties) to change the order or use only one, or set `provider` on a single mod.

CurseForge needs an API key. Release builds include one, so it works without setup. To use your own, set `SHULKER_CURSEFORGE_KEY` or run [`shulker config set curseforge.key <key>`](/docs/cli#shulker-config-set); your key always takes priority and is never replaced. If CurseForge rejects the included key, shulker fetches a new one from shulker.sh, saves it in its cache, and retries once. If that fails too, it asks you to set your own key or report an issue.

## Cache

Every file shulker downloads is stored once, by hash, in a cache shared by all your projects: `~/Library/Caches/shulker` on macOS, or wherever `SHULKER_CACHE` points. A build places files out of it, so a mod ten instances use is downloaded once, and installing a pack you have built before needs no network at all.

Nothing shulker placed is deleted without its bytes reaching the cache first. That is what makes [`shulker rollback`](/docs/cli#shulker-rollback) work offline: a history entry leaves mod and pack files out and relies on the cache to put them back.

[`shulker cache prune`](/docs/cli#shulker-cache-prune) removes what nothing references. What counts as a reference is every registered instance and the project you run it in — each one's `shulker.lock`, the lock of every history entry it keeps, and the modpack checkouts and offline sync fallbacks its sources need. A registered folder that is gone is skipped; one whose lock can't be read stops the prune instead. Your CurseForge key and the Java runtimes shulker manages for servers are never touched. [`shulker cache info`](/docs/cli#shulker-cache-info) shows what it would free before you run it.
