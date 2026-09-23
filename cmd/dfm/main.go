// Command dfm manages dotfiles: GNU stow style symlinking plus the repo
// bookkeeping the old bin/dotfiles script did, in one binary.
package main

import (
	"os"

	"github.com/tammersaleh/dotfiles-manager/cmd"
)

func main() {
	os.Exit(cmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
