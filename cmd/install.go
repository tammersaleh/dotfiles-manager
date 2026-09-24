package cmd

import (
	"github.com/tammersaleh/dotfiles-manager/internal/output"
	"github.com/tammersaleh/dotfiles-manager/internal/stow"
)

// InstallCmd restows public then private into the target. Equivalent to
// `stow --dir=<root> --target=<target> --restow public private`.
type InstallCmd struct{}

func (c *InstallCmd) Run(cli *CLI) error {
	root, err := cli.RootDir()
	if err != nil {
		return err
	}
	target, err := cli.TargetDir()
	if err != nil {
		return err
	}
	p := cli.Printer()

	plan, err := stow.Restow(root, target, stow.Packages)
	if err != nil {
		return err
	}
	if len(plan.Conflicts) > 0 {
		for _, cf := range plan.Conflicts {
			if err := p.PrintError(&output.Error{Err: "conflict", Detail: cf.Detail, Hint: cf.Hint, Path: cf.Path}); err != nil {
				return err
			}
		}
		return &output.Reported{Code: output.ExitConflict}
	}

	for _, a := range plan.Actions {
		p.Progress("%s", describe(a))
		if err := p.Row(a); err != nil {
			return err
		}
	}
	if !cli.DryRun {
		if err := plan.Apply(func(op string) { p.Verbosef("%s", op) }); err != nil {
			return err
		}
	}

	m := plan.Meta
	switch {
	case len(plan.Actions) == 0:
		p.Progress("nothing to do")
	case cli.DryRun:
		p.Progress("dry run: %d actions planned (created %d, removed %d, unfolded %d, refolded %d), nothing changed",
			len(plan.Actions), m.Created, m.Removed, m.Unfolded, m.Refolded)
	default:
		p.Progress("created %d, removed %d, unfolded %d, refolded %d", m.Created, m.Removed, m.Unfolded, m.Refolded)
	}
	return p.PrintMeta(m)
}

// describe is the human progress line for one action.
func describe(a stow.Action) string {
	switch a.Kind {
	case "link":
		return "LINK " + a.Path + " -> " + a.Target
	case "unlink":
		if a.Reason != "" {
			return "UNLINK " + a.Path + " (" + a.Reason + ")"
		}
		return "UNLINK " + a.Path
	case "mkdir":
		return "MKDIR " + a.Path
	case "rmdir":
		return "RMDIR " + a.Path
	case "unfold":
		return "UNFOLD " + a.Path
	case "refold":
		return "REFOLD " + a.Path
	}
	return a.Kind + " " + a.Path
}
