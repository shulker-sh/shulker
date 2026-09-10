# Concepts

## Manifest

`shulker.json` declares the Minecraft version, loader, targets, mods, and packs. See the [manifest reference](/docs/manifest).

## Lock

`shulker.lock.json` records the exact resolved versions, hashes, and pack commits. See the [lock reference](/docs/lock).

## Targets

A target is a build output: a client instance or a server directory.

## Packs

A pack is another shulker project whose mods and overrides merge into this one.

## Providers

Mods are resolved from Modrinth or CurseForge.
