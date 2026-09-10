# CLI reference

| Command | Description |
| --- | --- |
| `shulker init` | Create shulker.json and a lock in the current directory |
| `shulker add <mod>...` | Add mods to the manifest and lock |
| `shulker remove <mod>...` | Remove mods from the manifest and lock |
| `shulker update [mod...]` | Update mods to the newest compatible version |
| `shulker outdated [mod...]` | Show mods with a newer compatible version |
| `shulker pin <mod> [version]` | Pin a mod to a provider version id |
| `shulker unpin <mod>` | Remove a mod's pin and re-resolve it |
| `shulker install` | Download everything in the lock and build all targets |
| `shulker build [target]` | Assemble build directories from the lock and overrides |
| `shulker diff [target]` | Show build files that differ from what build would write |
| `shulker pull [file...]` | Copy edits made in a build directory back into their source |
| `shulker serve` | Build a server target and run it in the foreground |
| `shulker link` | Point a launcher at this project's client build |
| `shulker sync <source>` | Download and build one target of a project into a directory |
| `shulker pack add <source>` | Add a pack from a local path, git URL, or manifest URL |
| `shulker pack remove <name>` | Remove a pack |
| `shulker pack list` | List packs and their local drift state |
| `shulker player [name\|uuid]...` | Check player names and uuids against Mojang and the lock |
| `shulker import mrpack <file>` | Create a project from a Modrinth modpack |
| `shulker export mrpack` | Export a Modrinth modpack |
| `shulker version` | Print the shulker version |
