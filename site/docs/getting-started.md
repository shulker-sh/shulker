---
description: Install Shulker, create a project, add mods, and build a Minecraft client or server.
---

# Getting Started

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

The installer downloads the latest release and verifies its checksum, and when the [GitHub CLI](https://cli.github.com/) is installed it also checks the build provenance, then installs `shulker` to `~/.local/bin` (on Windows, `%LOCALAPPDATA%\Programs\shulker`) and adds that directory to your PATH. Once installed, `shulker self update` updates it to the latest Shulker release on GitHub. The archives, `checksums.txt` and the attestation bundle are on the [releases page](https://github.com/shulker-sh/shulker/releases), and the repository's [Building from Source](https://github.com/shulker-sh/shulker#building-from-source) covers `go install` and building from a clone.

## Create a Project

In an empty directory, create a manifest for Minecraft 26.3 with the latest version of the Fabric mod loader.

```sh
shulker create --fabric --minecraft 26.3
```

This writes `shulker.json`, which you edit and commit, and `shulker.lock`, which Shulker keeps up to date. The `create` command takes the manifest defaults for anything not given, so with no flags at all it makes a vanilla Minecraft client on the latest release, which takes no mods. Pass another loader such as `--neoforge` to start from something else. To walk through each choice interactively instead, run `shulker init`. Every default is listed under [`shulker create`](/docs/cli#shulker-create) in the CLI Reference, with [`shulker init`](/docs/cli#shulker-init) beside it.

## Add Mods

Add mods by their Modrinth or CurseForge slug. Dependencies are resolved and locked for you.

```sh
shulker add sodium lithium
```

Use `shulker outdated` to see available updates and `shulker update` to take them.

## Play the Client

Shulker is a launcher itself. Link the project to it and play.

```sh
shulker link shulker
shulker play
```

`link shulker` creates an instance under Shulker's own instances root that follows this project, and builds it before it returns. `play` syncs the instance from the project, fetches the Minecraft version, its libraries and the loader into a shared store, and starts the game. Launching needs a Microsoft account that owns Minecraft. See [Minecraft Accounts](#minecraft-accounts) below for how to sign one in, or how to use an account another launcher has already signed in.

Or link another launcher. Each `link` creates an instance that follows the project and syncs from it before every launch, so the launcher picks up each change you make here.

```sh
shulker link prism
```

Without a launcher, `shulker build` assembles every declared side into `build/<side>`, for checking what a build holds or [exporting](/docs/cli#shulker-export-mrpack) it.

To play someone else's modpack, give `link` its git or manifest URL. You don't need a project of your own. The instance follows that URL and syncs from it before each launch, in Shulker and in any other launcher alike.

```sh
shulker link shulker https://github.com/shulker-sh/base-pack.git
shulker link prism https://github.com/shulker-sh/base-pack.git
```

## Minecraft Accounts

Launching and playing Minecraft from the Shulker CLI needs a Microsoft account that owns [Minecraft: Java Edition](https://www.minecraft.net/en-us/store/minecraft-java-bedrock-edition-pc). Sign one in once, and every launch after that uses it.

```sh
shulker accounts login
```

Shulker then shows a Microsoft sign-in page to open and a code to enter there, which lets you play Minecraft through Shulker. Shulker stores only the sign-in tokens a launch needs, and never prints them. If only one account is signed in, every launch uses it.

If you are already signed in to another launcher (Prism Launcher, MultiMC, the Minecraft Launcher, ATLauncher or GDLauncher), Shulker can use that account instead of asking you to sign in again. Add the launcher to the stores Shulker reads accounts from, and its accounts appear beside your own. Then pick the default.

```console
❯ shulker accounts stores add prism
~ accounts.stores ["shulker"] ⟶ ["shulker","prism"]
❯ shulker accounts
     Account  UUID                                  Group     State
  ✔  Steve    8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21  own       playable
     Notch    069a79f4-44e9-4726-a5be-fca90e38aaf5  launcher  playable
❯ shulker accounts use Notch
✔ Notch is now the default account (069a79f4-44e9-4726-a5be-fca90e38aaf5)
```

Use `mojang` in place of `prism` for the Minecraft Launcher.

Offline accounts need a Microsoft account too. Shulker refuses to create one until it can see a signed-in account that owns Minecraft, in its own store or a launcher's. Sign in first, then add the offline account.

```console
❯ shulker accounts add Alex
✘ error: shulker can see no account that owns Minecraft: Java Edition, so it won't create an offline one (ownership-unproven)

Sign in to Microsoft:
  $ shulker accounts login
❯ shulker accounts login
✔ signed in as Steve (8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21)
❯ shulker accounts add Alex
✔ created the offline account Alex (36532b5e-c442-3dbb-a24c-c7e55d0f979a)
```

Every accounts command is described under [`shulker accounts`](/docs/cli#shulker-accounts) in the CLI Reference.

## Launchers

Shulker is a launcher itself, and it links into five others. A link creates an instance inside the launcher's own folder that follows your project, or any pack's git or manifest URL, and fills the launcher's pre-launch and post-exit command slots. Before each launch the instance syncs. It fetches the pack's latest lock from its source, downloads what changed, places it, and leaves any file you edited alone with a warning. A sync never keeps you from playing, since one that cannot reach its source warns and builds from the lock it already has.

| Launcher | Command | Syncs before each launch | Can use its accounts |
| --- | --- | --- | --- |
| [Shulker](https://shulker.sh) | `shulker link shulker` | yes | its own |
| [Prism Launcher](https://github.com/PrismLauncher/PrismLauncher) | `shulker link prism` | yes | yes |
| [MultiMC](https://github.com/MultiMC/Launcher) | `shulker link multimc` | yes | yes |
| [Minecraft Launcher](https://www.minecraft.net/en-us/download) | `shulker link mojang` | yes | yes |
| [ATLauncher](https://github.com/ATLauncher/ATLauncher) | `shulker link atlauncher` | yes | yes |
| [GDLauncher](https://github.com/gorilla-devs/GDLauncher-Carbon) | `shulker link gdlauncher` | yes, paused after four minutes | yes |
| [Modrinth App](https://github.com/modrinth/code), [CurseForge app](https://www.curseforge.com/download/app) | `shulker export mrpack`, `shulker export curseforge` | no | no |

Some launchers need a word more.

- **Minecraft Launcher** has no command slots, so Shulker points the profile's Java at a small shim of its own, which syncs, starts the real Java, and records the run. Pass `--no-hooks` to skip the shim and keep the launcher's own Java. Shulker also installs the loader version into the launcher for you.
- **ATLauncher and GDLauncher** read their instances at start, so restart them after a link to see the new instance. ATLauncher installs Forge and NeoForge through the loader's own installer, which Shulker runs once per version.
- **GDLauncher** kills a pre-launch command after five minutes, so Shulker pauses an update that is still downloading at four and tells you to launch again or run `shulker sync` to finish it.
- **MultiMC** has no default folder, so `shulker link multimc` needs `--launcher-dir`.
- **Modrinth App and the CurseForge app** cannot run a command before a launch, so Shulker only exports to them. The pack they import is a snapshot that does not follow your project.

Turn the hooks off with `--no-hooks`, or one of them with `--no-pre-launch` or `--no-post-exit`. Each `link` command is described under [`shulker link`](/docs/cli#shulker-link) in the CLI Reference.

## Run a Server

A server project is created the same way, with `--server`. `serve` builds it and starts it in the foreground, downloading what the lock needs first.

```sh
shulker create --server --fabric --minecraft 26.3
shulker add lithium
shulker serve
```

An existing project gains a server side when you add `"server": {}` to its `shulker.json`. The first `serve` asks you to accept the [Minecraft EULA](https://aka.ms/MinecraftEULA) and records your answer in your Shulker config, so no project asks again. Client-only mods stay out of the server build.

## Next Steps

- [Concepts](/docs/concepts) explains manifests, locks, sides, modpacks, and instances.
- The [CLI Reference](/docs/cli) covers every command.
- The [Manifest Reference](/docs/manifest) lists every field in `shulker.json`.
