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

In an empty directory, create a manifest with the latest Minecraft release, Fabric, and a client target:

```sh
shulker init --yes
```

This writes `shulker.json`, which you edit and commit, and `shulker.lock`, which shulker keeps up to date. Pass `--minecraft`, `--loader`, or `--target server` to start from something else. See [`shulker init`](/docs/cli#shulker-init).

## Add mods

Add mods by their Modrinth slug. Dependencies are resolved and locked for you:

```sh
shulker add sodium lithium
```

Use `shulker outdated` to see available updates and `shulker update` to take them.

## Build the client

```sh
shulker build client
```

This assembles the mods and everything in `overrides/` into `build/client`. To play it, point a launcher at the build:

```sh
shulker link prism
```

This works with Prism Launcher. For the official launcher, use `shulker link mojang`.

To play someone else's pack, give `link` its git or manifest URL. You don't need a project of your own. A Prism instance syncs from that URL before each launch; the official launcher has no pre-launch hook, so that profile updates when you run `shulker sync --all`:

```sh
shulker link prism https://github.com/shulker-sh/base-pack.git
shulker link mojang https://github.com/shulker-sh/base-pack.git
```

## Run a server

Add a server target, then build and start it in one step:

```sh
shulker target add server
shulker serve
```

The first run asks you to accept the [Minecraft EULA](https://aka.ms/MinecraftEULA) and records your answer in `shulker.json`. Client-only mods are left out of the server build automatically.

## Next steps

- [Concepts](/docs/concepts) explains manifests, locks, targets, and packs.
- The [CLI reference](/docs/cli) covers every command.
- The [manifest reference](/docs/manifest) lists every field in `shulker.json`.
