package cmd

// StatusCmd reports per-package git state and pending conflicts. Stub until
// feature 3 lands.
type StatusCmd struct{}

func (c *StatusCmd) Run(cli *CLI) error {
	return notImplemented("status")
}
