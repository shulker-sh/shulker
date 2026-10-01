# Changelog

All notable changes to shulker are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

The first release of shulker, a package manager for Minecraft modpacks. A pack is a `shulker.json` you edit and a `shulker.lock` that records exact versions and hashes, so every machine builds the same pack.

### Added

- Projects for Fabric, Quilt, NeoForge and Forge, back to Forge 1.12.2: `create` or `init` start one, and `add`, `remove`, `update`, `outdated`, `pin` and `lock` manage mods from Modrinth, CurseForge or local files, and `set`, `get` and `unset` edit any manifest field against its [schema](https://shulker.sh/docs/manifest).
- `search` across Modrinth and CurseForge at once, showing a mod both list as one row.
- Dependency checks from each jar's own metadata, run for the client and the server separately, with `ignore` to accept a known problem and `check` to fail a CI run on any of them ([GitHub Actions](https://shulker.sh/docs/github-actions)). Fabric Loader's and NeoForge's dependency override files are applied when a build places one.
- Client and server builds from one project, with override folders per side and per feature, owned files like `options.txt` and `server.properties` merged per key so in-game edits survive a rebuild, and `diff` and `pull` to bring those edits back.
- Modpacks layered in from local folders, git repositories, manifest URLs, `.mrpack` and CurseForge zips, or a Modrinth or CurseForge slug or page URL.
- Resource packs, shaders and datapacks as entries of their own, placed and enabled by the build.
- Mods gated on a named feature or the operating system, switched per machine with `feature on|off`.
- `serve` runs a server on Mojang's Java runtime, and `player` manages its whitelist, ops and bans.
- `play` launches the pack with no other launcher involved, signing in to Microsoft with `accounts login` or using the accounts other launchers already hold.
- `link` keeps Prism Launcher, MultiMC, ATLauncher, GDLauncher and the official launcher in sync with the pack before each launch, with the pack's icon and the instance's own memory, JVM arguments and window size applied in each.
- The pack's own entry in the in-game mod list, with its links, its icon and a month of what each sync changed.
- History and `rollback` for instances, shared save groups with `backup` and `restore`, and a download cache shared between projects.
- `import` and `export` for Modrinth `.mrpack` and CurseForge modpack zips, and `match` to lock the jars in an existing pack's folders. Importing a pack again replaces what the earlier import wrote and keeps what you changed.
- `security` lists what shulker does to keep a bad file off your machine, and `audit` reports what in a pack deserves a closer look ([Security](https://shulker.sh/docs/security)).
- `--json` on every command with stable error codes, `docs` for the documentation offline, shell completions, and `self update`.
- Installers for macOS, Linux and Windows ([Getting Started](https://shulker.sh/docs/getting-started)).

[Unreleased]: https://github.com/shulker-sh/shulker/commits/master
