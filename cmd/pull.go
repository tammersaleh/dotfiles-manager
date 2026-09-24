package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tammersaleh/dotfiles-manager/internal/gitx"
	"github.com/tammersaleh/dotfiles-manager/internal/hook"
	"github.com/tammersaleh/dotfiles-manager/internal/output"
	"github.com/tammersaleh/dotfiles-manager/internal/stow"
)

// hookName is the per-package hook pull runs after install.
const hookName = "post-pull.sh"

// PullCmd updates both packages, relinks, and runs post-pull hooks.
//
// Both packages are checked for uncommitted changes before either is
// fetched; a dirty package is a fatal dirty_tree and nothing has changed.
// Then, public first: fetch, rebase onto FETCH_HEAD, echo `git status
// --short`. A git failure stops before the next package. Then install,
// exactly as `dfm install`. Then each package's post-pull.sh, if present and
// executable, with cwd set to the package. A hook that exits non-zero is a
// fatal hook_failed and the next hook does not run. The bash script's
// stash/pop around the rebase is gone on purpose (SPEC.md "dfm pull").
type PullCmd struct {
	NoHooks bool `help:"Skip post-pull.sh."`
}

// pullRow is the --json row per package rebased. After is null under
// --dry-run, where nothing is fetched.
type pullRow struct {
	Action  string  `json:"action"` // "pull"
	Package string  `json:"package"`
	Before  string  `json:"before"`
	After   *string `json:"after"`
}

// hookRow is the --json row per hook run. Exit is null under --dry-run.
type hookRow struct {
	Action  string `json:"action"` // "hook"
	Package string `json:"package"`
	Exit    *int   `json:"exit"`
}

// skipRow is the --json row per hook not run: "absent", "not_executable",
// or "no_hooks".
type skipRow struct {
	Action  string `json:"action"` // "skip"
	Package string `json:"package"`
	Reason  string `json:"reason"`
}

func (c *PullCmd) Run(cli *CLI) error {
	root, err := cli.RootDir()
	if err != nil {
		return err
	}
	target, err := cli.TargetDir()
	if err != nil {
		return err
	}
	rootAbs, err := realDir(root, "root")
	if err != nil {
		return err
	}
	if err := stow.CheckPackages(root, rootAbs, stow.Packages); err != nil {
		return err
	}
	p := cli.Printer()

	if err := c.checkClean(p, rootAbs); err != nil {
		return err
	}

	var meta output.Meta
	for _, pkg := range stow.Packages {
		if err := c.pullPackage(cli, p, rootAbs, pkg, &meta); err != nil {
			return err
		}
	}

	installMeta, err := installWith(cli, p, root, target, "the packages are already updated; rerun dfm install after fixing")
	if err != nil {
		return err
	}
	meta.Created, meta.Removed = installMeta.Created, installMeta.Removed
	meta.Unfolded, meta.Refolded = installMeta.Unfolded, installMeta.Refolded

	for _, pkg := range stow.Packages {
		if err := c.runHook(cli, p, rootAbs, pkg, &meta); err != nil {
			return err
		}
	}
	return p.PrintMeta(meta)
}

// checkClean runs git status in every package and returns dirty_tree
// naming each dirty one. Nothing is fetched until this passes.
func (c *PullCmd) checkClean(p *output.Printer, rootAbs string) error {
	var dirty []string
	var changes int
	for _, pkg := range stow.Packages {
		dir := filepath.Join(rootAbs, pkg)
		p.Verbosef("git status --porcelain --untracked-files (%s)", dir)
		tree, err := gitx.Status(dir)
		if err != nil {
			return err
		}
		if tree.Dirty {
			dirty = append(dirty, pkg)
			changes += len(tree.Lines)
		}
	}
	if len(dirty) == 0 {
		return nil
	}
	path := rootAbs
	if len(dirty) == 1 {
		path = filepath.Join(rootAbs, dirty[0])
	}
	return &output.Error{
		Err:    "dirty_tree",
		Detail: fmt.Sprintf("%s: uncommitted changes (%s); nothing was fetched", strings.Join(dirty, ", "), plural(changes, "change")),
		Hint:   "commit or discard the changes in the package, then rerun dfm pull",
		Path:   path,
		Code:   output.ExitGeneral,
	}
}

// pullPackage fetches and rebases one package and echoes its short status.
// Under --dry-run it only reports what would happen.
func (c *PullCmd) pullPackage(cli *CLI, p *output.Printer, rootAbs, pkg string, meta *output.Meta) error {
	dir := filepath.Join(rootAbs, pkg)
	before, err := gitx.HeadSHA(dir)
	if err != nil {
		return prefixGitError(err, pkg)
	}
	if cli.DryRun {
		p.Progress("dry run: would fetch %s and rebase onto FETCH_HEAD", pkg)
		meta.Pulled++
		return p.Row(pullRow{Action: "pull", Package: pkg, Before: before})
	}

	p.Progress("pulling %s...", pkg)
	p.Verbosef("git fetch --all --quiet (%s)", dir)
	if err := gitx.Fetch(dir); err != nil {
		return prefixGitError(err, pkg)
	}
	p.Verbosef("git rebase FETCH_HEAD --quiet (%s)", dir)
	if err := gitx.Rebase(dir, "FETCH_HEAD"); err != nil {
		return prefixGitError(err, pkg)
	}
	after, err := gitx.HeadSHA(dir)
	if err != nil {
		return prefixGitError(err, pkg)
	}
	if after == before {
		p.Progress("%s: already up to date", pkg)
	} else {
		p.Progress("%s: %s..%s", pkg, short(before), short(after))
	}
	meta.Pulled++
	if err := p.Row(pullRow{Action: "pull", Package: pkg, Before: before, After: &after}); err != nil {
		return err
	}

	// The one wrapped command whose output is the result (SPEC.md
	// "Output"). Dropped under --json, where it would corrupt the stream.
	p.Verbosef("git status --short --untracked-files (%s)", dir)
	status, err := gitx.ShortStatus(dir)
	if err != nil {
		return prefixGitError(err, pkg)
	}
	if status != "" {
		p.Result("%s", strings.TrimRight(status, "\n"))
	}
	return nil
}

// runHook runs <pkg>/post-pull.sh when present and executable. The hook's
// stdout goes to dfm's stdout in human mode and to stderr under --json so
// the JSONL stream stays clean; its stderr is always dfm's stderr.
func (c *PullCmd) runHook(cli *CLI, p *output.Printer, rootAbs, pkg string, meta *output.Meta) error {
	dir := filepath.Join(rootAbs, pkg)
	name := pkg + "/" + hookName
	if c.NoHooks {
		p.Verbosef("skipping %s: --no-hooks", name)
		return p.Row(skipRow{Action: "skip", Package: pkg, Reason: "no_hooks"})
	}
	path := filepath.Join(dir, hookName)
	ok, err := hook.Executable(path)
	if err != nil {
		return &output.Error{Err: "hook_failed", Detail: fmt.Sprintf("%s: %v", name, err), Hint: "check the hook file, or pass --no-hooks", Path: path}
	}
	if !ok {
		reason := "absent"
		if _, statErr := os.Stat(path); statErr == nil {
			reason = "not_executable"
		}
		p.Verbosef("skipping %s: %s", name, strings.ReplaceAll(reason, "_", " "))
		return p.Row(skipRow{Action: "skip", Package: pkg, Reason: reason})
	}

	if cli.DryRun {
		p.Progress("dry run: would run %s", name)
		meta.Hooks++
		return p.Row(hookRow{Action: "hook", Package: pkg})
	}
	p.Progress("running %s", name)
	stdout := p.Out
	if p.JSON {
		stdout = p.Err
	}
	code, err := hook.Run(path, dir, stdout, p.Err)
	if err != nil {
		return &output.Error{Err: "hook_failed", Detail: fmt.Sprintf("%s: %v", name, err), Hint: "check the hook file, or pass --no-hooks", Path: path}
	}
	if rowErr := p.Row(hookRow{Action: "hook", Package: pkg, Exit: &code}); rowErr != nil {
		return rowErr
	}
	if code != 0 {
		return &output.Error{
			Err:    "hook_failed",
			Detail: fmt.Sprintf("%s exited %d", name, code),
			Hint:   "the packages are already updated and linked; fix the hook and rerun dfm pull, or pass --no-hooks",
			Path:   path,
			Code:   output.ExitGeneral,
		}
	}
	meta.Hooks++
	return nil
}

// prefixGitError names the package in a gitx error's detail.
func prefixGitError(err error, pkg string) error {
	if oErr, ok := err.(*output.Error); ok {
		oErr.Detail = pkg + ": " + oErr.Detail
	}
	return err
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
