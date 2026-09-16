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

Every mod has a side: `client`, `server`, or `both`. Shulker reads it from the provider, and you can override it with `side` on the mod. A target only gets mods for its own side plus those marked `both`, so client-only mods like shaders never end up on a server.

## Overrides

Overrides are folders of files copied into a build on top of the mods: configs, resource packs, scripts. A target lists its layers in order and later layers win, so a shared `overrides/common` can sit under a `overrides/server`. Files ending in `.tmpl` have `${name}` replaced with the target's [`variables`](/docs/manifest#variables) and are written without the suffix.

Some files are generated from `shulker.json` instead: `server.properties`, `options.txt`, and the whitelist, ops, and ban lists.

## Edits in the build directory

Each build records what it wrote in `.shulker-state.json` in the build directory, so the next build can tell your edits apart from its own files. That way config you change in-game survives:

- A file you edited is kept, as long as its source hasn't changed.
- If the file changed in both places, or a file shulker didn't write is in the way, the build stops and lists it. Use `shulker build --force` to overwrite.
- Generated files like `server.properties` merge per key, so a key you edited in-game and a key you changed in `shulker.json` both apply. If both changed the same key, `shulker.json` wins and the build warns.
- A `.properties` file in your overrides merges per key too. shulker manages only the keys it lists, and leaves any others a mod writes alone. A file the mod wrote first gets those keys merged in instead of stopping the build. List a path in the target's [`wholeFiles`](/docs/manifest) to copy it whole instead.

Use [`shulker diff`](/docs/cli#shulker-diff) to see what changed, and [`shulker pull`](/docs/cli#shulker-pull) to copy those edits back into your overrides or `shulker.json` so they're part of the project.

## Modpacks

A modpack is another shulker project whose mods and overrides merge into this one. Its overrides sit beneath your own layers. A modpack that ships a `shulker.lock` is **locked**: the exact versions it pins, dependencies included, are copied into your lock and marked with the modpack they came from, and its Minecraft and loader have to match yours exactly — or, when you set neither, yours are taken from it, and every locked modpack has to agree. One with no lock, or added with `--unlocked`, is **floating**: its mods are resolved here alongside your own, and its versions only have to fit your ranges. A mod you list in `shulker.json` yourself always wins over either. Add one from a local path, git URL, or manifest URL with [`shulker modpack add`](/docs/cli#shulker-modpack-add-remove-list).

## Providers

Mods are resolved from Modrinth or CurseForge. By default Modrinth is tried first, then CurseForge. Set [`providers`](/docs/manifest#properties) to change the order or use only one, or set `provider` on a single mod.

CurseForge needs an API key. Release builds include one, so it works without setup. To use your own, set `SHULKER_CURSEFORGE_KEY` or run [`shulker config set curseforge.key <key>`](/docs/cli#shulker-config-set); your key always takes priority and is never replaced. If CurseForge rejects the included key, shulker fetches a new one from shulker.sh, saves it in its cache, and retries once. If that fails too, it asks you to set your own key or report an issue.
