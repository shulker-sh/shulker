---
description: Export a Shulker modpack and publish a GitHub release from GitHub Actions whenever you push a version tag.
---

# GitHub Actions

Release a modpack from its repository: push a tag like `v1.2.0`, and a workflow exports the Modrinth and CurseForge archives and attaches them to a GitHub release.

## The workflow

Save this as `.github/workflows/release.yml` in the pack's repository:

```yaml
name: Release

on:
  push:
    tags: ["v*"]

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0

      - id: shulker
        uses: shulker-sh/setup-shulker@v1
        with:
          version: v0.0.1

      - name: Export
        run: |
          shulker export mrpack --version "${GITHUB_REF_NAME#v}"
          shulker export curseforge --version "${GITHUB_REF_NAME#v}"

      - name: Release notes
        run: |
          prev=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || true)
          git log --pretty='- %s' "${prev:+$prev..}HEAD" > notes.md

      - name: Release
        run: gh release create "$GITHUB_REF_NAME" build/*.mrpack build/*.zip --notes-file notes.md
        env:
          GH_TOKEN: ${{ github.token }}
```

Then tag and push:

```sh
git tag v1.2.0
git push origin v1.2.0
```

## Install

[`shulker-sh/setup-shulker`](https://github.com/shulker-sh/setup-shulker) installs Shulker on Linux, macOS and Windows runners and puts it on the `PATH` for later steps. `version` pins the release it installs, so a new Shulker release never changes your builds until you bump it. Leave it out to install the latest release. The action checks each download against the release's checksums and its build provenance, and fails the step if either doesn't match.

## Cache

Export reads every locked file from Shulker's cache, and downloads the ones a fresh checkout is missing. The action keeps that cache between runs, keyed on `shulker.lock`: while the lock is unchanged the export downloads nothing, and after a change it starts from the previous cache and downloads only what's new. It then prunes the files the new lock no longer needs, so the cache doesn't grow with every change.

A pack that isn't at the root of its repository names its lock with `lock-files`, one path per line. A repository holding several packs lists each one's lock. The cache is then keyed on all of them, and a prune keeps what any of them needs:

```yaml
      - id: shulker
        uses: shulker-sh/setup-shulker@v1
        with:
          version: v0.0.1
          lock-files: |
            packs/survival/shulker.lock
            packs/creative/shulker.lock
```

Set `prune: false` to keep every file a restored cache holds.

## Export

`--version "${GITHUB_REF_NAME#v}"` takes the pack version from the tag, so `v1.2.0` exports `1.2.0` and `shulker.json` needs no `version` of its own. The archives land in `build/` as `<name>-<version>.mrpack` and `<name>-<version>.zip`. Both carry `shulker.json` and `shulker.lock`, and the same project exports byte-identical archives on every run.

Export never relocks. When `shulker.lock` doesn't match `shulker.json`, it fails with `lock-stale`: run `shulker lock` locally and commit the lock.

### Files that aren't on CurseForge

`export curseforge` refers to each file by its CurseForge file ID. A file locked from Modrinth or anywhere else is looked up on CurseForge by its fingerprint, and one that isn't there fails the export. Pass `--bundle` to put those files inside the archive instead; the CurseForge app warns about them on import. `export mrpack --bundle` does the same for files Modrinth launchers won't download.

## Release notes

Shulker doesn't write a changelog for a pack. The workflow lists the commit subjects since the previous tag, which is why the checkout fetches the full history with `fetch-depth: 0`. To write the notes by hand instead, keep them in a file in the repository and pass that to `--notes-file`.

## Publish to Modrinth and CurseForge

[mc-publish](https://github.com/Kir-Antipov/mc-publish) uploads the archives to Modrinth and CurseForge. It can't read a modpack's Minecraft version or loader, so pass it the action's `minecraft-version` and `loader` outputs. The action reads them from the lock, like [`shulker get --locked`](/docs/cli#shulker-get): `shulker.json` may hold a range such as `*`, and the lock holds the exact version. With several lock files, the action leaves these outputs empty, so read each pack's with `shulker get --locked` instead. Add these steps after the release notes:

```yaml
      - name: Pack version
        id: pack
        run: echo "version=${GITHUB_REF_NAME#v}" >> "$GITHUB_OUTPUT"

      - uses: Kir-Antipov/mc-publish@v3.3
        with:
          modrinth-id: AABBCCDD
          modrinth-token: ${{ secrets.MODRINTH_TOKEN }}
          modrinth-files: build/*.mrpack
          curseforge-id: 123456
          curseforge-token: ${{ secrets.CURSEFORGE_TOKEN }}
          curseforge-files: build/*.zip
          version: ${{ steps.pack.outputs.version }}
          changelog-file: notes.md
          loaders: ${{ steps.shulker.outputs.loader }}
          game-versions: ${{ steps.shulker.outputs.minecraft-version }}
```

The IDs are your project's on each site, and the tokens are your own upload tokens, stored as repository secrets.
