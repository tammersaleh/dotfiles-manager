package cmd

// IgnoreCmd appends the path to the owning package's .gitignore. Stub until
// feature 5 lands.
type IgnoreCmd struct {
	Path string `arg:"" help:"Path under the target to ignore."`
}

func (c *IgnoreCmd) Run(cli *CLI) error {
	return notImplemented("ignore")
}
