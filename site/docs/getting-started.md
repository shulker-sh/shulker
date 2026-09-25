---
description: Install Shulker, create a project, add mods, and build a Minecraft client or server.
---

# Getting started

Install the CLI, create a project, add mods, and build a client or server.

## Install

::: code-group

```sh [macOS / Linux]
curl -fsSL https://shulker.sh/install.sh | sh
```

```powershell [Windows]
irm https://shulker.sh/install.ps1 | iex
```

:::

The installer downloads the latest release, checks it against the published SHA256 checksums, and installs `shulker` to `~/.local/bin` (on Windows, `%LOCALAPPDATA%\Programs\shulker`), adding that directory to your PATH.

## Create a project

In an empty directory, create a manifest with the latest Minecraft release, Fabric, and a client side:

```sh
shulker create --fabric
```

This writes `shulker.json`, which you edit and commit, and `shulker.lock`, which shulker keeps up to date. Pass `--minecraft`, another loader such as `--neoforge`, or `--side server` to start from something else; with no loader the project is vanilla Minecraft, which takes no mods. On a terminal, `shulker init` asks about each of these instead. See [`shulker create`](/docs/cli#shulker-create) and [`shulker init`](/docs/cli#shulker-init).

## Add mods

Add mods by their Modrinth or CurseForge slug. Dependencies are resolved and locked for you:

```sh
shulker add sodium lithium
```

Use `shulker outdated` to see available updates and `shulker update` to take them.

## Play the client

Shulker is a launcher itself. Link the project to it and play:

```sh
shulker link shulker
shulker play
```

`link shulker` creates an instance under shulker's own instances root that follows this project, and builds it before it returns. `play` syncs the instance from the project, fetches the Minecraft version, its libraries and the loader into a shared store, and starts the game. It needs an account: [`shulker accounts login`](/docs/cli#shulker-accounts-login) signs a Microsoft account in once, and every launch after that uses it.

Or link another launcher. Each `link` creates an instance that follows the project and syncs from it before every launch, so the launcher picks up each change you make here:

```sh
shulker link prism
```

This works with Prism Launcher; `mojang` (the official launcher), `multimc`, `atlauncher` and `gdlauncher` work the same way. Without a launcher, `shulker build` assembles every declared side into `build/<side>`, for checking what a build holds or [exporting](/docs/cli#shulker-export-mrpack) it.

To play someone else's modpack, give `link` its git or manifest URL. You don't need a project of your own: the instance follows that URL and syncs from it before each launch, in shulker and in any other launcher alike:

```sh
shulker link shulker https://github.com/shulker-sh/base-pack.git
shulker link prism https://github.com/shulker-sh/base-pack.git
```

## Run a server

Declare a server side by adding `"server": {}` to `shulker.json`, then build and start it in one step:

```sh
shulker serve
```

The first run asks you to accept the [Minecraft EULA](https://aka.ms/MinecraftEULA) and records your answer in `shulker.json`. Client-only mods are left out of the server build automatically.

## Next steps

- [Concepts](/docs/concepts) explains manifests, locks, sides, modpacks, and instances.
- The [CLI reference](/docs/cli) covers every command.
- The [manifest reference](/docs/manifest) lists every field in `shulker.json`.
