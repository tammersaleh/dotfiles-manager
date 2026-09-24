package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tammersaleh/dotfiles-manager/internal/output"
	"github.com/tammersaleh/dotfiles-manager/internal/stow"
)

// PublicCmd moves a path from the target into the public package and runs
// install so the path is linked back. SPEC.md "dfm public <path> and dfm
// private <path>".
type PublicCmd struct {
	Path string `arg:"" help:"Path under the target to adopt."`
}

func (c *PublicCmd) Run(cli *CLI) error { return adopt(cli, "public", c.Path) }

// PrivateCmd is PublicCmd for the private package.
type PrivateCmd struct {
	Path string `arg:"" help:"Path under the target to adopt."`
}

func (c *PrivateCmd) Run(cli *CLI) error { return adopt(cli, "private", c.Path) }

// adoptRow is the --json row for the move itself. From is target-relative,
// To is root-relative.
type adoptRow struct {
	Action  string `json:"action"` // "adopt"
	Package string `json:"package"`
	Path    string `json:"path"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// adopt resolves arg against the target, refuses anything outside the
// target, inside the root, missing, or already tracked, then moves it into
// pkg and runs install. Install is planned before the move so a
// pre-existing conflict elsewhere in the target (exit 2) moves nothing.
func adopt(cli *CLI, pkg, arg string) error {
	root, err := cli.RootDir()
	if err != nil {
		return err
	}
	target, err := cli.TargetDir()
	if err != nil {
		return err
	}
	p := cli.Printer()

	rootAbs, err := realDir(root, "root")
	if err != nil {
		return err
	}
	targetAbs, err := realDir(target, "target")
	if err != nil {
		return err
	}

	src, rel, err := resolveAdoptPath(arg, rootAbs, targetAbs)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return &output.Error{Err: "not_found", Detail: fmt.Sprintf("%s does not exist", src),
			Hint: "check the path; it is resolved against the target when relative", Path: rel}
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		if owner := linkOwner(src, rootAbs); owner != "" {
			return &output.Error{Err: "already_tracked",
				Detail: fmt.Sprintf("%s is a symlink into package %s", rel, owner),
				Hint:   fmt.Sprintf("it is already managed by dfm; edit it in %s", filepath.Join(rootAbs, owner)), Path: rel}
		}
	}
	dest := filepath.Join(rootAbs, pkg, rel)
	if _, err := os.Lstat(dest); err == nil {
		return &output.Error{Err: "already_tracked",
			Detail: fmt.Sprintf("%s already exists in package %s", rel, pkg),
			Hint:   fmt.Sprintf("remove one copy, then run dfm install (the %s copy is at %s)", pkg, dest), Path: rel}
	}
	if fi.IsDir() {
		if tracked := trackedInside(src, rootAbs); tracked != "" {
			trackedRel, _ := filepath.Rel(targetAbs, tracked)
			return &output.Error{Err: "contains_tracked",
				Detail: fmt.Sprintf("%s holds %s, a symlink into the dotfiles root", rel, trackedRel),
				Hint:   "adopt the untracked children individually", Path: rel}
		}
	}

	// Plan on the current tree first: a conflict anywhere blocks the move.
	plan, err := stow.Restow(root, target, stow.Packages)
	if err != nil {
		return err
	}
	if len(plan.Conflicts) > 0 {
		return reportConflicts(p, plan, "")
	}

	p.Progress("moving %s to %s", rel, pkg)
	if err := p.Row(adoptRow{Action: "adopt", Package: pkg, Path: rel, From: rel, To: filepath.Join(pkg, rel)}); err != nil {
		return err
	}
	if cli.DryRun {
		// The planner reads the filesystem, so the install that follows the
		// move cannot be planned without moving. Say so and stop.
		p.Progress("dry run: would move %s to %s and run install, nothing changed", src, dest)
		return p.PrintMeta(output.Meta{Adopted: 1})
	}

	p.Verbosef("mkdir -p %s", filepath.Dir(dest))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return &output.Error{Err: "adopt_failed", Detail: err.Error(), Path: rel,
			Hint: "nothing was moved"}
	}
	p.Verbosef("rename %s -> %s", src, dest)
	if err := os.Rename(src, dest); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return &output.Error{Err: "cross_device", Path: rel,
				Detail: fmt.Sprintf("%s and %s are on different filesystems", src, dest),
				Hint:   "the target and the dotfiles root must share one filesystem; copy the path in by hand and rerun dfm install"}
		}
		return &output.Error{Err: "adopt_failed", Detail: err.Error(), Path: rel,
			Hint: "nothing was moved"}
	}

	meta, err := install(cli, p, root, target)
	if err != nil {
		return err
	}
	meta.Adopted = 1
	return p.PrintMeta(meta)
}

// resolveAdoptPath turns the argument into (absolute source, target-relative
// path). Relative arguments resolve against the target. The parent
// directory's symlinks are resolved so the inside-target and inside-root
// checks see the real location; the leaf is not resolved because a symlink
// is a legitimate thing to adopt.
func resolveAdoptPath(arg, rootAbs, targetAbs string) (string, string, error) {
	path := arg
	if !filepath.IsAbs(path) {
		path = filepath.Join(targetAbs, path)
	}
	path = filepath.Clean(path)
	parentReal, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", "", &output.Error{Err: "not_found", Detail: fmt.Sprintf("%s does not exist", filepath.Dir(path)),
			Hint: "check the path; it is resolved against the target when relative", Path: arg}
	}
	src := filepath.Join(parentReal, filepath.Base(path))

	if !inside(src, targetAbs) {
		return "", "", &output.Error{Err: "outside_target", Path: arg,
			Detail: fmt.Sprintf("%s resolves to %s, which is not inside %s", arg, src, targetAbs),
			Hint:   "only paths under the target can be adopted"}
	}
	if src == rootAbs || inside(src, rootAbs) {
		e := &output.Error{Err: "inside_root", Path: arg,
			Detail: fmt.Sprintf("%s resolves to %s, which is inside the dotfiles root", arg, src),
			Hint:   "it is already part of a package; edit it there"}
		if pkgRel, err := filepath.Rel(rootAbs, src); err == nil {
			if pkg := strings.SplitN(pkgRel, string(filepath.Separator), 2)[0]; pkg != "." && pkg != "" {
				e.Hint = fmt.Sprintf("it is already tracked by package %s; edit it there", pkg)
			}
		}
		return "", "", e
	}
	rel, err := filepath.Rel(targetAbs, src)
	if err != nil {
		return "", "", &output.Error{Err: "outside_target", Detail: err.Error(), Path: arg}
	}
	return src, rel, nil
}

// trackedInside walks dir and returns the first symlink that points into
// the root, or "". Adopting such a directory would move dfm's own links
// into the package.
func trackedInside(dir, rootAbs string) string {
	var found string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 && linkOwner(path, rootAbs) != "" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
