<p align="center"><img src="assets/icon.png" alt="" width="128"></p>

<h1 align="center">Shulker</h1>

<p align="center">
  <a href="https://github.com/shulker-sh/shulker/releases"><img alt="Release" src="https://img.shields.io/github/v/release/shulker-sh/shulker?style=for-the-badge"></a>
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-yellow?style=for-the-badge"></a>
</p>

<p align="center">One manifest for your Minecraft mods, client instances, and servers.</p>

Shulker is a command-line tool that keeps a modpack in one file, `shulker.json`, and does everything from there. Name the Minecraft version, the loader and the mods you want, and Shulker fetches them from Modrinth and CurseForge, checks their dependencies against the jars' own metadata, and records exact versions and hashes in `shulker.lock`, so the pack builds the same way on every machine. From that one manifest it builds a client and links it into a launcher, runs a server, and exports a `.mrpack` or CurseForge pack. It is a launcher itself, and links into [five others](#launchers).

_Shulker is not an official Minecraft product. It is not associated with Mojang or Microsoft._

## Installation

**macOS and Linux**

```sh
curl -fsSL https://shulker.sh/install.sh | sh
```

**Windows**

```powershell
irm https://shulker.sh/install.ps1 | iex
```

Both verify the release's checksum, and when the [GitHub CLI](https://cli.github.com/) is installed they also check its build provenance, then install `shulker` to a directory of your own and add it to your PATH. Once installed, running `shulker self update` will update it to the latest Shulker release on GitHub. The archives, `checksums.txt` and the attestation bundle are on the [releases page](https://github.com/shulker-sh/shulker/releases) for anyone who would rather not pipe curl into a shell.

## Building from Source

```sh
go install shulker.sh/shulker@latest
```

Or clone the repository and build it yourself.

```sh
git clone https://github.com/shulker-sh/shulker
cd shulker
go build .
go test ./...
```

Only [Go](https://github.com/golang/go) is required. A source build carries no CurseForge API key, so to use CurseForge, set `SHULKER_CURSEFORGE_KEY` or run `shulker config set curseforge.key <key>`. To get a CurseForge API key, sign in to the [CurseForge for Studios console](https://console.curseforge.com/) and create one under API keys. A Shulker built from source cannot self-update. Running `shulker self update` from a source build will print what needs to be done in order to update, which is another `go install` or a `git pull` and build.

## Examples

These commands create a new Shulker manifest in the current directory, using Minecraft 26.2 with the latest version of the Fabric mod loader, add both the [Sodium](https://modrinth.com/mod/sodium) and [Lithium](https://modrinth.com/mod/lithium) mods, and launch the game.

```sh
shulker create --fabric --minecraft 26.2
shulker add sodium lithium
shulker link shulker
shulker play
```

Launching the game needs a Microsoft account that owns Minecraft. See [Minecraft Accounts](#minecraft-accounts) below for how to sign one in, or how to use an account another launcher has already signed in.

The `create` command will use the manifest defaults unless overridden. If you want to interactively walk through these options, run the `shulker init` command instead. With no flags at all, `create` makes a vanilla Minecraft client on the latest release. Every default is listed [in the CLI reference](https://shulker.sh/docs/cli#shulker-create).

To create a server manifest instead, run these commands.

```sh
shulker create --server --fabric --minecraft 26.2
shulker add lithium
shulker serve
```

The first `serve` asks you to accept the [Minecraft EULA](https://aka.ms/MinecraftEULA). Client-only mods stay out of the server build.

To play an existing modpack from Modrinth using Prism Launcher, import it by its Modrinth slug into an empty directory and link that.

```sh
shulker import fabulously-optimized
shulker link prism
```

The `import` command also takes a `.mrpack` or CurseForge zip, by path or URL, and turns it into a project of your own.

A pack that is already a Shulker project, one with a `shulker.json` in a git repository, needs no import. Give `link` its URL and the instance follows it, syncing from it before every launch.

```sh
shulker link prism https://github.com/shulker-sh/base-pack.git
```

## Launchers

Shulker is a launcher itself, and it links into five others. A link creates an instance inside the launcher's own folder that follows your project, or any pack's git or manifest URL, and fills the launcher's pre-launch and post-exit command slots. Before each launch the instance syncs. It fetches the pack's latest lock from its source, downloads what changed, places it, and leaves any file you edited alone with a warning. A sync never keeps you from playing, since one that cannot reach its source warns and builds from the lock it already has.

| Launcher | Command | Syncs Before Each Launch | Can Use Its Accounts |
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

Turn the hooks off with `--no-hooks`, or one of them with `--no-pre-launch` or `--no-post-exit`. Each `link` command is described in the [CLI Reference](https://shulker.sh/docs/cli#shulker-link).

## Minecraft Accounts

Launching and playing Minecraft from the Shulker CLI needs a Microsoft account that owns [Minecraft: Java Edition](https://www.minecraft.net/en-us/store/minecraft-java-bedrock-edition-pc). Sign one in once, and every launch after that uses it.

```sh
shulker accounts login
```

Shulker then shows a Microsoft sign-in page to open and a code to enter there, which lets you play Minecraft through Shulker. Shulker stores only the sign-in tokens a launch needs, and never prints them. If only one account is signed in, every launch uses it.

If you are already signed in to another launcher (Prism Launcher, MultiMC, the Minecraft Launcher, ATLauncher or GDLauncher), Shulker can use that account instead of asking you to sign in again. Add the launcher to the stores Shulker reads accounts from, and its accounts appear beside your own. Then pick the default.

```console
❯ shulker accounts stores add prism
~ accounts.stores ["shulker"] → ["shulker","prism"]
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
✘ No signed-in account owns Minecraft: Java Edition (ownership-unproven)

Sign in to Microsoft:
  $ shulker accounts login
❯ shulker accounts login
✔ signed in as Steve (8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21)
❯ shulker accounts add Alex
✔ created the offline account Alex (36532b5e-c442-3dbb-a24c-c7e55d0f979a)
```

## Commands

These commands are what most projects need.

| Command | What It Does |
| --- | --- |
| `shulker create` | Write `shulker.json` and `shulker.lock` from flags, taking the defaults for anything not given, without asking |
| `shulker init` | The same, asking about each choice on the terminal |
| `shulker add <mod>...` | Add mods by Modrinth or CurseForge slug or URL, resolving and locking their dependencies |
| `shulker update` | Move every mod, and any modpack the project follows, to its newest compatible version. `shulker outdated` shows what it would change |
| `shulker link <launcher>` | Create an instance in a launcher that follows the project and syncs from it before each launch |
| `shulker play` | Build, fetch and start the instance Shulker owns for the project |
| `shulker sync` | Update a linked instance from its pack with `-i`, or build a project, git URL or manifest URL straight into a directory |
| `shulker serve` | Build the server side and run it in the foreground, downloading what the lock needs first |
| `shulker import <pack>` | Create a project from a `.mrpack`, a CurseForge zip or a Modrinth slug, or merge one into the project |
| `shulker export mrpack\|curseforge` | Export the project as a Modrinth `.mrpack`, or the client as a CurseForge profile zip |
| `shulker accounts` | List every account Shulker can see, with `login`, `add`, `use` and `stores` under it |

To see the full list of commands, run `shulker --help` or read the [CLI Reference](https://shulker.sh/docs/cli).

## Documentation

- [Getting Started](https://shulker.sh/docs/getting-started) walks through installation, a first project, playing and serving.
- [Concepts](https://shulker.sh/docs/concepts) explains manifests, locks, sides, modpacks and instances.
- The [CLI Reference](https://shulker.sh/docs/cli) covers every command, and the [Manifest Reference](https://shulker.sh/docs/manifest) every field of `shulker.json`.
- The [changelog](CHANGELOG.md) lists what each release adds.

## License

Shulker is released under the [MIT License](LICENSE).
