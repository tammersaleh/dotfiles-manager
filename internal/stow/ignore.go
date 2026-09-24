package stow

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tammersaleh/dotfiles-manager/internal/output"
)

// LocalIgnoreFile is the per-package ignore list stow reads.
const LocalIgnoreFile = ".stow-local-ignore"

// defaultIgnoreList is stow 2.4.1's built-in list (the __DATA__ section of
// Stow.pm), used when a package has no .stow-local-ignore.
const defaultIgnoreList = `# Comments and blank lines are allowed.

RCS
.+,v

CVS
\.\#.+       # CVS conflict files / emacs lock files
\.cvsignore

\.svn
_darcs
\.hg

\.git
\.gitignore
\.gitmodules

.+~          # emacs backup files
\#.*\#       # emacs autosave files

^/README.*
^/LICENSE.*
^/COPYING
`

// IgnoreList is a compiled .stow-local-ignore. Patterns without a slash
// match the basename of a path; patterns with a slash match the whole
// package-relative path with a leading slash. Both alternations are built
// exactly as Stow.pm compile_ignore_regexps does.
type IgnoreList struct {
	path    *regexp.Regexp // (^|/)(p1|p2)(/|$) against "/" + relpath
	segment *regexp.Regexp // ^(s1|s2)$ against the basename
}

// LoadIgnoreList compiles pkgDir/.stow-local-ignore, or stow's default list
// when the file is absent. A line that does not compile is a fatal
// bad_ignore_pattern error naming the file and line.
func LoadIgnoreList(pkgDir string) (*IgnoreList, error) {
	file := filepath.Join(pkgDir, LocalIgnoreFile)
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return parseIgnoreList(strings.NewReader(defaultIgnoreList), "<built-in>")
	}
	if err != nil {
		return nil, &output.Error{Err: "io_error", Detail: err.Error(), Path: file}
	}
	defer func() { _ = f.Close() }()
	return parseIgnoreList(f, file)
}

// parseIgnoreList mirrors get_ignore_regexps_from_fh and
// compile_ignore_regexps: trim, drop blank and comment lines, strip
// trailing comments, unescape \#, always add the local ignore file itself,
// then split into segment and path alternations.
func parseIgnoreList(r io.Reader, file string) (*IgnoreList, error) {
	seen := map[string]bool{}
	var patterns []string
	sc := bufio.NewScanner(r)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = trailingComment.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, `\#`, "#")
		pattern := translatePerlRegexp(line)
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, &output.Error{
				Err:    "bad_ignore_pattern",
				Detail: fmt.Sprintf("%s:%d: %s: %v", file, lineNo, line, err),
				Hint:   "fix or remove the pattern; stow ignore lists are Perl regexps and dfm accepts the RE2 subset",
				Path:   file,
			}
		}
		if !seen[pattern] {
			seen[pattern] = true
			patterns = append(patterns, pattern)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, &output.Error{Err: "io_error", Detail: err.Error(), Path: file}
	}
	// Local ignore lists always stay within the package.
	self := "^/" + regexp.QuoteMeta(LocalIgnoreFile) + "$"
	if !seen[self] {
		patterns = append(patterns, self)
	}
	sort.Strings(patterns) // order does not affect matching; keep it stable

	var segments, paths []string
	for _, p := range patterns {
		if strings.Contains(p, "/") {
			paths = append(paths, p)
		} else {
			segments = append(segments, p)
		}
	}
	l := &IgnoreList{}
	if len(segments) > 0 {
		l.segment = regexp.MustCompile("^(" + strings.Join(segments, "|") + ")$")
	}
	if len(paths) > 0 {
		l.path = regexp.MustCompile("(^|/)(" + strings.Join(paths, "|") + ")(/|$)")
	}
	return l, nil
}

// trailingComment is Stow.pm's `s/\s+#.+//`.
var trailingComment = regexp.MustCompile(`\s+#.+`)

// translatePerlRegexp maps the Perl regexp features the ignore lists might
// reasonably use onto RE2. Anything else fails to compile and is reported
// as bad_ignore_pattern.
func translatePerlRegexp(p string) string {
	p = strings.ReplaceAll(p, `\Z`, `$`)
	return p
}

// Match reports whether the package-relative path (no leading slash, as
// stow_contents builds it) is ignored.
func (l *IgnoreList) Match(rel string) bool {
	if l.path != nil && l.path.MatchString("/"+rel) {
		return true
	}
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	return l.segment != nil && l.segment.MatchString(base)
}
