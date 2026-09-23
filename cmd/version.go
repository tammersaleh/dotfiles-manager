package cmd

import (
	"runtime/debug"

	"github.com/tammersaleh/dotfiles-manager/internal/output"
)

// Version is set by GoReleaser via
// -ldflags "-X github.com/tammersaleh/dotfiles-manager/cmd.Version=...".
// Empty means a local build; resolveVersion then falls back to build info.
var Version = ""

// VersionCmd prints the version. Human mode: the bare version on stdout.
// --json: {"version":"..."} then the _meta trailer.
type VersionCmd struct{}

func (c *VersionCmd) Run(cli *CLI) error {
	p := cli.Printer()
	v := resolveVersion(Version, debug.ReadBuildInfo)
	p.Result("%s", v)
	if err := p.Row(map[string]string{"version": v}); err != nil {
		return err
	}
	return p.PrintMeta(output.Meta{})
}

// resolveVersion picks, in order: the ldflags value, the main module version
// from build info (a `go install module@vX.Y.Z` build), else "dev".
// `go build` and `go test` report "(devel)" or empty, which count as unset.
func resolveVersion(ldflags string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if ldflags != "" {
		return ldflags
	}
	if bi, ok := readBuildInfo(); ok {
		v := bi.Main.Version
		if v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
