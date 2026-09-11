package main

import (
	"os"

	"shulker.sh/shulker/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
