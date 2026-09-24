package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tammersaleh/dotfiles-manager/internal/output"
)

// Path helpers shared by the commands that take a path argument (public,
// private, ignore).

// realDir is dir made absolute with symlinks resolved.
func realDir(dir, what string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		return "", &output.Error{Err: what + "_missing", Detail: err.Error(), Hint: "pass --" + what, Path: dir}
	}
	return abs, nil
}

// inside reports whether path is strictly below dir. Both are clean and
// absolute.
func inside(path, dir string) bool {
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}

// linkOwner returns the package a symlink at link points into, or "".
// Relative link text is joined with the link's directory; absolute text is
// taken as is. Neither is resolved further, matching stow's textual
// ownership check.
func linkOwner(link, rootAbs string) string {
	dest, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(link), dest)
	}
	dest = filepath.Clean(dest)
	if !inside(dest, rootAbs) {
		return ""
	}
	pkgRel, _ := filepath.Rel(rootAbs, dest)
	return strings.SplitN(pkgRel, string(filepath.Separator), 2)[0]
}
