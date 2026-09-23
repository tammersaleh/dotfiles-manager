package cmd

// PublicCmd moves a path into the public package and links it back. Stub
// until feature 4 lands.
type PublicCmd struct {
	Path string `arg:"" help:"Path under the target to adopt."`
}

func (c *PublicCmd) Run(cli *CLI) error {
	return notImplemented("public")
}

// PrivateCmd is PublicCmd for the private package.
type PrivateCmd struct {
	Path string `arg:"" help:"Path under the target to adopt."`
}

func (c *PrivateCmd) Run(cli *CLI) error {
	return notImplemented("private")
}
