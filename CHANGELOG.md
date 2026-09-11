# Changelog

All notable changes to shulker are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `init` creates a `shulker.json` manifest; `add`, `remove`, `update`, `outdated`, `pin` and `unpin` manage mods, with exact versions and hashes recorded in `shulker.lock`.
- Mods from Modrinth and CurseForge, with provider fallthrough, manual downloads for files CurseForge won't serve, and `add --provider` to switch a locked mod.
- Dependency checks from each jar's own metadata on `add` and `install`; `remove` prunes dependencies nothing else needs.
- Client and server targets (`target add|remove|list`), built with `install` and `build` into `build/<name>`.
- Server builds with the locked server jar, `eula.txt` and validated `server.properties`; `serve` runs them on Mojang's Java runtime.
- `player` resolves names against Mojang and writes the whitelist, ops and bans.
- `client.options` merges into `options.txt`, and `.properties` and other owned files merge per key, so in-game edits survive a rebuild; `diff` and `pull` bring them back into the project.
- Per-target data directories linked into builds, so worlds, logs and screenshots survive rebuilds.
- `pack add|remove|list` layers other projects in from local paths, git repositories or manifest URLs.
- Mods gated on OS or a named feature; `feature on|off|reset|list` saves per-machine choices in `shulker.local.json`, and `--with`, `--without` and `--os` override them for one run.
- A ModMenu entry with the pack's name, version, description, authors and links, which badges managed mods and turns off their update checks.
- `sync` builds a target of a local project, git repository or manifest URL into any directory, and falls back to the last successful sync when the network is down (`--offline` skips the network).
- `link prism`, `link multimc` and `link mojang` (alias `vanilla`) create instances or profiles that sync before each launch; `link prism` also takes a git or manifest URL.
- `links` lists linked instances and synced directories; `sync --instance`, `sync --all` and a picker update them by name; `unlink` stops syncing one.
- `export mrpack` and `import mrpack` for Modrinth modpacks.
- `--json` on every command: one envelope with the result, every warning and, on failure, a stable error code; the codes are listed in the CLI reference.
- JSON Schemas for `shulker.json` and `shulker.lock` at `https://shulker.sh/schema/v1/`.
- Installers for macOS and Linux (`curl -fsSL https://shulker.sh/install.sh | sh`) and Windows (`irm https://shulker.sh/install.ps1 | iex`), and `self update`, all checking releases against their SHA256 checksums and, when `gh` is installed, their build provenance.

[Unreleased]: https://github.com/shulker-sh/shulker/commits/master
