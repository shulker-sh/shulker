# Changelog

All notable changes to shulker are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `add` looks up every name before failing, and adds nothing when one isn't found or has no compatible version: the error lists each and gives the command that adds the rest. `--skip-missing` adds the rest and warns about each one skipped.
- Importing a pack again replaces the entries and override files the earlier import wrote and you haven't changed, keeps only yours, and never goes back to an older version. RLCraft re-imported as 2.9.2d after 2.9.3, since CurseForge tags 2.9.3 with no loader.

## [0.0.1] - 2026-09-26

The first release of shulker, a package manager for Minecraft modpacks. A pack is a `shulker.json` you edit and a `shulker.lock` that records exact versions and hashes, so every machine builds the same pack.

### Added
- Projects for Fabric, Quilt, NeoForge and Forge, back to Forge 1.12.2: `create` or `init` start one, and `add`, `remove`, `update`, `outdated`, `pin` and `lock` manage mods from Modrinth, CurseForge or local files, and `set`, `get` and `unset` edit any manifest field against its [schema](https://shulker.sh/docs/manifest).
- Dependency checks from each jar's own metadata, run for the client and the server separately, with `ignore` to accept a known problem and `check` to fail a CI run on any of them ([GitHub Actions](https://shulker.sh/docs/github-actions)).
- Client and server builds from one project, with override folders per side and per feature, owned files like `options.txt` and `server.properties` merged per key so in-game edits survive a rebuild, and `diff` and `pull` to bring those edits back.
- Modpacks layered in from local folders, git repositories, manifest URLs, `.mrpack` and CurseForge zips, or a Modrinth or CurseForge slug.
- Resource packs, shaders and datapacks as entries of their own, placed and enabled by the build.
- Mods gated on a named feature or the operating system, switched per machine with `feature on|off`.
- `serve` runs a server on Mojang's Java runtime, and `player` manages its whitelist, ops and bans.
- `play` launches the pack with no other launcher involved, signing in to Microsoft with `accounts login` or using the accounts other launchers already hold.
- `link` keeps Prism Launcher, MultiMC, ATLauncher, GDLauncher and the official launcher in sync with the pack before each launch.
- History and `rollback` for instances, shared save groups with `backup` and `restore`, and a download cache shared between projects.
- `import` and `export` for Modrinth `.mrpack` and CurseForge modpack zips, and `match` to lock the jars in an existing pack's folders.
- `--json` on every command with stable error codes, `docs` for the documentation offline, shell completions, and `self update`.
- Installers for macOS, Linux and Windows ([Getting Started](https://shulker.sh/docs/getting-started)).

[Unreleased]: https://github.com/shulker-sh/shulker/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/shulker-sh/shulker/releases/tag/v0.0.1
