package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tammersaleh/dotfiles-manager/internal/gitx"
	"github.com/tammersaleh/dotfiles-manager/internal/output"
	"github.com/tammersaleh/dotfiles-manager/internal/stow"
)

// StatusCmd reports, per package, the repository state (dirty, ahead,
// behind) and what the next install would hit in the target: conflicts and
// broken links. Read-only: it plans the restow and never applies it, and
// git runs with GIT_OPTIONAL_LOCKS=0. The report is the result, so it goes
// to stdout in both modes. Exit 2 when a conflict would block install.
type StatusCmd struct{}

// packageRow is the --json row per package.
type packageRow struct {
	Kind     string  `json:"kind"` // "package"
	Package  string  `json:"package"`
	Dirty    bool    `json:"dirty"`
	Changes  int     `json:"changes"`  // porcelain lines
	Upstream *string `json:"upstream"` // null when the branch has none
	Ahead    *int    `json:"ahead"`
	Behind   *int    `json:"behind"`
}

// conflictRow is the --json row per conflict.
type conflictRow struct {
	Kind    string `json:"kind"` // "conflict"
	Package string `json:"package"`
	Path    string `json:"path"`
	Detail  string `json:"detail"`
	Hint    string `json:"hint"`
}

// brokenLinkRow is the --json row per dangling link into the root.
type brokenLinkRow struct {
	Kind    string `json:"kind"` // "broken_link"
	Package string `json:"package"`
	Path    string `json:"path"`
	Target  string `json:"target"` // link text
}

func (c *StatusCmd) Run(cli *CLI) error {
	root, err := cli.RootDir()
	if err != nil {
		return err
	}
	target, err := cli.TargetDir()
	if err != nil {
		return err
	}
	p := cli.Printer()

	// Plan first: it validates the packages (package_missing) before any
	// git process runs, and nothing is applied.
	plan, err := stow.Restow(root, target, stow.Packages)
	if err != nil {
		return err
	}

	var meta output.Meta
	var rows []any
	for _, pkg := range stow.Packages {
		dir := filepath.Join(root, pkg)
		p.Verbosef("git status --porcelain --untracked-files (%s)", dir)
		tree, err := gitx.Status(dir)
		if err != nil {
			return err
		}
		p.Verbosef("git rev-list --left-right --count @{upstream}...HEAD (%s)", dir)
		sync, err := gitx.AheadBehind(dir)
		if err != nil {
			return err
		}
		if tree.Dirty {
			meta.Dirty++
		}
		row := packageRow{Kind: "package", Package: pkg, Dirty: tree.Dirty, Changes: len(tree.Lines)}
		if sync.Upstream != "" {
			row.Upstream, row.Ahead, row.Behind = &sync.Upstream, &sync.Ahead, &sync.Behind
		}
		rows = append(rows, row)
	}

	for _, cf := range plan.Conflicts {
		rows = append(rows, conflictRow{Kind: "conflict", Package: cf.Package, Path: cf.Path, Detail: cf.Detail, Hint: cf.Hint})
	}
	meta.Conflicts = len(plan.Conflicts)
	for _, a := range plan.Actions {
		if a.Kind != "unlink" || a.Reason != "source_missing" {
			continue
		}
		dest, err := os.Readlink(filepath.Join(plan.Target, a.Path))
		if err != nil {
			return &output.Error{Err: "plan_failed", Detail: err.Error(), Path: a.Path}
		}
		rows = append(rows, brokenLinkRow{Kind: "broken_link", Package: a.Package, Path: a.Path, Target: dest})
		meta.BrokenLinks++
	}
	meta.Pending = len(plan.Actions)

	// Every row is collected before anything is written, so a git failure
	// on the second package leaves stdout empty and stderr holding only the
	// fatal error.
	for _, r := range rows {
		p.Result("%s", describeRow(r))
		if err := p.Row(r); err != nil {
			return err
		}
	}
	switch {
	case meta.Conflicts > 0:
		p.Result("install is blocked by %s", plural(meta.Conflicts, "conflict"))
	case meta.Pending > 0:
		p.Result("install would apply %s", plural(meta.Pending, "action"))
	}
	if err := p.PrintMeta(meta); err != nil {
		return err
	}
	if meta.Conflicts > 0 {
		return &output.Reported{Code: output.ExitConflict}
	}
	return nil
}

// describeRow is the human line for one status row.
func describeRow(r any) string {
	switch r := r.(type) {
	case packageRow:
		tree := "clean"
		if r.Dirty {
			tree = "dirty (" + plural(r.Changes, "change") + ")"
		}
		var sync string
		switch {
		case r.Upstream == nil:
			sync = "no upstream"
		case *r.Ahead == 0 && *r.Behind == 0:
			sync = "in sync with " + *r.Upstream
		default:
			var parts []string
			if *r.Ahead > 0 {
				parts = append(parts, fmt.Sprintf("ahead %d", *r.Ahead))
			}
			if *r.Behind > 0 {
				parts = append(parts, fmt.Sprintf("behind %d", *r.Behind))
			}
			sync = strings.Join(parts, ", ") + " of " + *r.Upstream
		}
		return r.Package + ": " + tree + ", " + sync
	case conflictRow:
		return "conflict " + r.Path + ": " + r.Detail + " (" + r.Hint + ")"
	case brokenLinkRow:
		return "broken link " + r.Path + " -> " + r.Target + " (" + r.Package + ")"
	}
	return fmt.Sprint(r)
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
