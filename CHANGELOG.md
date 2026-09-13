# Changelog

All notable changes to shulker are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `init` creates a `shulker.json` manifest; `add`, `remove`, `update`, `outdated`, `pin` and `unpin` manage mods, with exact versions and hashes recorded in `shulker.lock`.
- Dependency problems found in jar metadata fail the command that would lock them and print the `shulker ignore` line that accepts each one; `unignore` drops it again. An ignore records the range the jar declared, so a new version that declares something else brings the problem back.
- `lock` brings `shulker.lock` in line with hand edits to `shulker.json` and changes to local packs without upgrading anything, and the out-of-date warning names each difference.
- `set`, `unset` and `get` edit and read any field of `shulker.json` by dotted path, checked against the schema before anything is written.
- A `shulker.json`, `shulker.lock`, `shulker.local.json`, `config.json` or `registry.json` that doesn't parse is reported with its line and column and what was expected there, and a manifest or lock that doesn't match its schema lists each failing field by dotted path.
- `config get|set|unset` read and change shulker's own `config.json`: the CurseForge key, masked unless `--reveal`, and where the registry lives, which needs `--force` when linked instances would be left behind.
- Fabric, Quilt, NeoForge and Forge projects (`init --loader`); on Quilt, mods without a Quilt build use their Fabric one.
- NeoForge and Forge servers and launcher profiles set up by the loader's own installer: servers run it offline from files recorded in `shulker.lock`, and `link mojang` installs the client into the official launcher without leaving the installer's own profile behind.
- Mods from Modrinth and CurseForge, with provider fallthrough, manual downloads for files CurseForge won't serve, and `add --provider` to switch a locked mod.
- CurseForge works without your own API key: release builds include one, and if CurseForge rejects it shulker fetches a replacement from shulker.sh.
- Dependency checks from each jar's own metadata on `add` and `install`; `remove` prunes dependencies nothing else needs.
- `suggests` lists the mods locked mods recommend and that aren't installed; `--optional` adds their optional integrations.
- Client and server targets (`target add|remove|list`), built with `install` and `build` into `build/<name>`.
- Server builds with the locked server jar, `eula.txt` and validated `server.properties`; `serve` runs them on Mojang's Java runtime.
- `player` resolves names against Mojang and writes the whitelist, ops and bans.
- `client.options` merges into `options.txt`, and `.properties` and other owned files merge per key, so in-game edits survive a rebuild; `diff` and `pull` bring them back into the project.
- Per-target data directories linked into builds, so worlds, logs and screenshots survive rebuilds.
- `pack add|remove|list` layers other projects in from local paths, git repositories or manifest URLs.
- Mods gated on OS or a named feature; `feature on|off|reset|list` saves per-machine choices in `shulker.local.json`, and `--with`, `--without` and `--os` override them for one run.
- A ModMenu entry with the pack's name, version, description, authors and links, which badges managed mods and turns off their update checks; NeoForge and Forge packs get the same summary in the mod list.
- `sync` builds a target of a local project, git repository or manifest URL into any directory, and falls back to the last successful sync when the network is down (`--offline` skips the network).
- `link prism`, `link multimc`, `link atlauncher` and `link mojang` (alias `vanilla`) create instances or profiles for the client build; a Prism or ATLauncher instance syncs before each launch, and ATLauncher needs no clicks to set one up, NeoForge and Forge included, and shows the shulker image until you pick your own. Every `link` takes a project directory, git URL, or manifest URL, so a pack can be played without a project of your own; a remote `link mojang` keeps its game directory under `shulker/` in the launcher folder.
- `links` lists linked instances and synced directories; `sync --instance`, `sync --all` and a picker update them by name, and a bare `sync` inside a project updates just that project's entries; `unlink` stops syncing one, named or, inside a project, by launcher (`unlink mojang`).
- `export mrpack` and `import mrpack` for Modrinth modpacks.
- `export curseforge` writes a CurseForge profile `.zip` the CurseForge app imports. Mods from Modrinth or a URL are matched to CurseForge files by fingerprint; `--bundle` ships the ones that aren't on CurseForge inside the archive.
- `export mrpack` and `export curseforge` take a project directory, git URL or manifest URL to export from, with `--ref`.
- `--json` on every command: one envelope with the result, every warning and, on failure, a stable error code; the codes are listed in the CLI reference, and the plain error line ends with the same code.
- JSON Schemas for `shulker.json` and `shulker.lock` at `https://shulker.sh/schema/v1/`.
- Human output with one style throughout: green, yellow and red `+ ~ -` change lines, `✔` and `!` result lines, an error tree under `✘ error:` with the code, steps that spin while they run and settle into grey `✔` lines, a download bar on terminals that fills by bytes with sizes recorded in `shulker.lock`, paths that click where the terminal supports links, and `--help` in the same style with the commands grouped by task. `--no-color` (or `NO_COLOR`) and `--ascii` turn colour and the Unicode glyphs off.
- Installers for macOS and Linux (`curl -fsSL https://shulker.sh/install.sh | sh`) and Windows (`irm https://shulker.sh/install.ps1 | iex`), and `self update`, all checking releases against their SHA256 checksums and, when `gh` is installed, their build provenance.

[Unreleased]: https://github.com/shulker-sh/shulker/commits/master
