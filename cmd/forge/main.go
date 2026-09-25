package main

import (
	"os"

	"github.com/MjxUpUp/Forge/internal/cli"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cli.SetVersion(version, commit, date)
	cli.Execute()
	os.Exit(0)
}
