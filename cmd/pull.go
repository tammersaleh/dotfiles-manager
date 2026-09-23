package cmd

// PullCmd updates both packages, relinks, and runs post-pull hooks. Stub
// until feature 6 lands.
type PullCmd struct {
	NoHooks bool `help:"Skip post-pull.sh."`
}

func (c *PullCmd) Run(cli *CLI) error {
	return notImplemented("pull")
}
