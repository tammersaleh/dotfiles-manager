package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tammersaleh/dotfiles-manager/internal/output"
)

// IgnoreCmd appends `/<path>` to the .gitignore of the package that owns
// the path. SPEC.md "dfm ignore <path>". Nothing else changes: no install
// follows, matching the script.
type IgnoreCmd struct {
	Path string `arg:"" help:"Path under the target to ignore."`
}

// ignoreRow is the --json row. Path is package-relative, File is
// root-relative, Line is the exact .gitignore line.
type ignoreRow struct {
	Action  string `json:"action"` // "ignore" or "noop"
	Package string `json:"package"`
	Path    string `json:"path"`
	File    string `json:"file"`
	Line    string `json:"line"`
	Reason  string `json:"reason,omitempty"` // "already_ignored" on noop
}

func (c *IgnoreCmd) Run(cli *CLI) error {
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

	pkg, rel, err := resolveOwner(c.Path, rootAbs, targetAbs)
	if err != nil {
		return err
	}

	line := "/" + filepath.ToSlash(rel)
	file := filepath.Join(rootAbs, pkg, ".gitignore")
	fileRel := filepath.Join(pkg, ".gitignore")

	content, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return &output.Error{Err: "ignore_failed", Detail: err.Error(), Path: fileRel,
			Hint: "nothing was written"}
	}
	if hasLine(string(content), line) {
		p.Progress("already ignored: %s is in %s", line, fileRel)
		if err := p.Row(ignoreRow{Action: "noop", Package: pkg, Path: rel, File: fileRel, Line: line, Reason: "already_ignored"}); err != nil {
			return err
		}
		return p.PrintMeta(output.Meta{})
	}

	p.Progress("adding %s to %s", line, fileRel)
	if err := p.Row(ignoreRow{Action: "ignore", Package: pkg, Path: rel, File: fileRel, Line: line}); err != nil {
		return err
	}
	if cli.DryRun {
		p.Progress("dry run: would append %s to %s, nothing changed", line, file)
		return p.PrintMeta(output.Meta{Ignored: 1})
	}

	var b strings.Builder
	if len(content) > 0 && content[len(content)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString(line)
	b.WriteByte('\n')
	p.Verbosef("append %q to %s", line, file)
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err == nil {
		_, err = f.WriteString(b.String())
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return &output.Error{Err: "ignore_failed", Detail: err.Error(), Path: fileRel,
			Hint: "check permissions on the package directory"}
	}
	return p.PrintMeta(output.Meta{Ignored: 1})
}

// hasLine reports whether content holds line exactly, ignoring trailing
// whitespace on each line. Comments and unanchored variants do not count.
func hasLine(content, line string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimRight(l, " \t\r") == line {
			return true
		}
	}
	return false
}

// resolveOwner turns the argument into (package, package-relative path).
//
// The argument is resolved like adopt: relative joins the target, and when
// the lexical path is neither under the target nor under the root the
// parent's symlinks are resolved once so an alias of the target still
// works. Anything still outside both is outside_target.
//
// A path under the root belongs to the package named by its first
// component. A path under the target is walked upward, one component at a
// time, until a component is a dfm-owned symlink (stow's textual check via
// linkOwner); the link's destination plus the remaining components is the
// package-relative path. A real file with no owned ancestor is
// not_tracked. Either way the package path must exist, else not_found.
func resolveOwner(arg, rootAbs, targetAbs string) (string, string, error) {
	path := arg
	if !filepath.IsAbs(path) {
		path = filepath.Join(targetAbs, path)
	}
	path = filepath.Clean(path)
	if !inside(path, targetAbs) && !inside(path, rootAbs) {
		if parentReal, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
			path = filepath.Join(parentReal, filepath.Base(path))
		}
	}

	notFound := func(where string) error {
		return &output.Error{Err: "not_found", Detail: fmt.Sprintf("%s does not exist", where),
			Hint: "check the path; it is resolved against the target when relative", Path: arg}
	}
	notTracked := func(detail string) error {
		return &output.Error{Err: "not_tracked", Detail: detail, Path: arg,
			Hint: "only paths linked from a package can be ignored; dfm public or dfm private adopts it first"}
	}

	if path == rootAbs || inside(path, rootAbs) {
		pkgRel, _ := filepath.Rel(rootAbs, path)
		parts := strings.SplitN(pkgRel, string(filepath.Separator), 2)
		if parts[0] == "." || len(parts) < 2 {
			return "", "", notTracked(fmt.Sprintf("%s is the dotfiles root or a package, not a path inside one", arg))
		}
		if _, err := os.Lstat(path); err != nil {
			return "", "", notFound(path)
		}
		return parts[0], parts[1], nil
	}

	if !inside(path, targetAbs) {
		return "", "", &output.Error{Err: "outside_target", Path: arg,
			Detail: fmt.Sprintf("%s resolves to %s, which is not inside %s", arg, path, targetAbs),
			Hint:   "only paths under the target or inside a package can be ignored"}
	}

	// Walk upward from the path to the first component below the target.
	for cur := path; cur != targetAbs; cur = filepath.Dir(cur) {
		fi, err := os.Lstat(cur)
		if err != nil {
			continue // missing leaf under a folded dir link; keep climbing
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			continue
		}
		dest, pkg := linkDest(cur, rootAbs)
		if pkg == "" {
			continue // foreign symlink; its parent may still be ours
		}
		rest, _ := filepath.Rel(cur, path)
		pkgPath := filepath.Join(dest, rest)
		pkgRel, _ := filepath.Rel(filepath.Join(rootAbs, pkg), pkgPath)
		if pkgRel == "." || pkgRel == ".." || strings.HasPrefix(pkgRel, ".."+string(filepath.Separator)) {
			return "", "", notTracked(fmt.Sprintf("%s resolves to %s, which is not inside package %s", arg, pkgPath, pkg))
		}
		if _, err := os.Lstat(pkgPath); err != nil {
			return "", "", notFound(fmt.Sprintf("%s (package %s path %s)", arg, pkg, pkgPath))
		}
		return pkg, pkgRel, nil
	}

	if _, err := os.Lstat(path); err != nil {
		return "", "", notFound(path)
	}
	rel, _ := filepath.Rel(targetAbs, path)
	return "", "", notTracked(fmt.Sprintf("%s is not a dfm symlink and has no dfm-owned ancestor", rel))
}
