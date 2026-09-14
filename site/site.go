package site

import "embed"

//go:embed docs/getting-started.md docs/concepts.md docs/cli.md docs/manifest.md docs/lock.md
var Docs embed.FS
