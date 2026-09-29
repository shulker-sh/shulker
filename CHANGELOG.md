# Changelog

All notable changes to shulker are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- A listing index in the cache remembers each Modrinth and CurseForge listing that `add`, `lock` or `import` proved to be the same, by a jar's mod id or a file's hash. `search` shows such a pair as one row, and adding the other listing of a locked mod no longer downloads its jar again. `cache info` counts the pairs and `cache prune` drops those unused for 90 days.

- `shulker security` explains how shulker keeps bad files off your machine: its stance, each protection and what it stops, and the settings that change them. Every security warning and error now ends by pointing at it, and under `--json` a security error names the protection that refused in `error.protection`.

- `shulker lock <key>` looks an entry up again from its provider at the version it is locked at and rewrites the file the lock names for it, leaving every other entry alone. It puts right a lock whose file doesn't match its provider without moving it to a newer version the way `update` does.

- A lock entry that names Modrinth or CurseForge has to download from that provider's own hosts: `build`, `install`, `sync` and a launch refuse one that doesn't as `provenance-mismatch`, and say who can fix it. For your own project that's `shulker lock <key>`; for a synced source, its author. A launch keeps its last good build and starts on it.

- Shulker holds back Modrinth and CurseForge versions published in the last 7 days when it chooses one, so a hijacked mod's new release has time to be caught before it reaches you. `add`, `update`, `lock` and a floating modpack take the newest version old enough, and say which they held back, how old each is, the day it qualifies and the `shulker pin` that takes it now; `outdated` marks them too. A mod with nothing old enough fails `release-too-new`. A pin takes its version whatever its age, with a warning, and `sync` installs a source's lock as its author locked it, warning about each entry that young. `security.minReleaseAge` in `config.json` sets the days, and `0` turns it off. Under `--json`, every security warning is also typed in the new `securityWarnings`, with its protection and facts.

- `shulker audit` reports what in a project, or an instance with `-i`, deserves a closer look: locked files Modrinth or CurseForge no longer has, lock entries that download from outside their provider or that it files under another project, jars and packs no provider published and where each comes from, jars in `mods/` that changed since shulker placed them or that the lock doesn't name, and versions younger than `security.minReleaseAge`. It asks each provider once for the whole lock and says when it couldn't, `audit <key>...` narrows it to those entries, and it fails for files gone from their provider and entries from outside it, so CI can gate on it. `shulker security` lists the check.

- `shulker cache verify` checks the whole cache: it rehashes every object, asks Modrinth and CurseForge once for every file a registered instance, the project here or any of their history entries locks and names the ones gone from their provider, and lists the objects nothing uses for `cache prune`. It fails on a changed object or a takedown, and `--fix` drops the changed objects so they download again.

- `sync`, the launcher hooks and `hook wrap` ask Modrinth and CurseForge once a day whether they still have each locked file, and warn about one gone from its provider or filed under another project, naming the instances that use it and the `audit`, `update` or `remove` to run. It's a warning, not a block: the cached copy is still placed. `build` repeats the last check's warning without going online.

- A build that finds a jar it placed in `mods/` changed since warns that it no longer matches the locked copy, rather than listing it as kept, and names `shulker audit <key>` and `--force`, which puts the locked copy back. The jar itself stays as it is. `--json` lists these as `changedJars`.

### Changed

- `add` looks up every name before failing, and adds nothing when one isn't found or has no compatible version: the error lists each and gives the command that adds the rest. `--skip-missing` adds the rest and warns about each one skipped.
- Importing a pack again replaces the entries and override files the earlier import wrote and you haven't changed, keeps only yours, and never goes back to an older version. RLCraft re-imported as 2.9.2d after 2.9.3, since CurseForge tags 2.9.3 with no loader.
- `-C` and `-i` belong to the commands that act on a project or instance, and go after the command: `shulker sync -i smp`, not `shulker -i smp sync`. Passing one to a command that ignored it, such as `version` or `accounts`, is now a usage error.
- `-C` has to name a directory that exists, or it is a usage error before the command runs, rather than a missing `shulker.json` or nothing at all. `init`, `create` and `import` still take a new one.
- `search -C` and `search -i` fail when the project they name can't be opened, instead of searching as if outside a project.

### Security

- A `.mrpack` whose index names a path outside the pack's folder is refused as `mrpack-invalid`: a `..` component, a leading `/` or `\`, a drive letter, or a Windows device name like `CON`. Such a pack could write files anywhere on `import`, or anywhere above the build on `add` then `build`.
- Shulker fetches over https only. A plain `http://` URL, whether a lock, a modpack archive or a redirect names it, fails as `url-insecure`.
- A git source must be `https://`, `ssh://`, `git@host:repo` or a local path. An `http://` or `git://` remote fails as `url-insecure`, and git is held to the same transports when it follows a redirect or a submodule.
- A `.mrpack` whose files download from anywhere but https on `cdn.modrinth.com`, `github.com`, `raw.githubusercontent.com` or `gitlab.com` is refused as `mrpack-invalid` on `import`, `add`, build and sync, as Modrinth launchers refuse it. Export bundles a file on an `http://` URL rather than linking it.
- A build checks each file it copies out of the cache against its hash. A changed copy is deleted and downloaded again with a warning, or fails the build as `cache-changed` when it has no URL, so one mod that rewrites the cache no longer reaches every instance built from it.
- `server.jvmArgs` is gone from the manifest, since a source's author could put `-javaagent:` or `-XX:OnOutOfMemoryError=` on your java command line. `server.memory` and the `server.jvmFlags` preset stay, as do your own `play.jvmArgs` and an instance's `jvmArgs`. A manifest that still has the key fails validation.
- A local file's path in `shulker.json` or `shulker.lock`, and a git modpack's `path`, must stay inside the project or repository on every OS: a `..` segment, a leading `/` or `\`, a colon, or a Windows device name like `CON` or `NUL` fails validation. Merging a source and copying a project's files refuse such a path as `path-outside`, and skip symlinks, so a source can no longer read or write files elsewhere on disk.
- A symlink in an override folder is skipped on build, rather than the file it points at being copied into the instance.

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
