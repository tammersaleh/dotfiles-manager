// Package stowtest is the step-driver test harness shared by the stow
// parity suite (layer 1) and the behavioral tests (layer 2). See SPEC.md
// "Testing".
//
// A Layout is a target directory with the dotfiles root inside it, as in
// production (~ and ~/dotfiles). Mutations edit a Layout; Snapshot records
// what is on disk so two layouts, or one layout before and after, can be
// diffed.
package stowtest

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// StowVersion is the only stow the parity suite trusts.
const StowVersion = "2.4.1"

// Layout is one target tree with its dotfiles root.
type Layout struct {
	Target string
	Root   string
}

// NewLayout creates <tmp>/home and <tmp>/home/dotfiles/{public,private}.
// Paths are symlink-resolved (macOS puts temp dirs under /var -> /private/var)
// so they compare equal to the paths dfm reports.
func NewLayout(t *testing.T) Layout {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, "home")
	root := filepath.Join(target, "dotfiles")
	for _, pkg := range []string{"public", "private"} {
		if err := os.MkdirAll(filepath.Join(root, pkg), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Layout{Target: target, Root: root}
}

// Mutation edits a layout. Every step of a fixture other than a Run is one.
type Mutation interface {
	Apply(t *testing.T, l Layout)
}

// Step is a Mutation or a Run.
type Step any

// Fixture is a named sequence of steps.
type Fixture struct {
	Name  string
	Steps []Step
}

// Run is a comparison point. The driver decides what to check; the fields
// carry layer 2 expectations and are ignored by the parity driver.
type Run struct {
	Args        []string // extra dfm flags, e.g. --dry-run, --json
	WantExit    int
	WantActions int      // planned action count; -1 to skip the check
	WantErrors  []string // error codes expected on stderr, in order
	WantTree    []Entry  // full expected target tree; nil to skip
	// StowDies marks a step where stow 2.4.1's --restow is known to die with
	// an internal error (a second package newly contributing to a folded
	// directory). The parity driver asserts the crash, then runs a plain
	// stow to recover the layout, and compares that against dfm.
	StowDies bool
}

// PkgFile writes a regular file into a package, creating parents.
type PkgFile struct {
	Pkg, Path, Content string
	Exec               bool
}

func (m PkgFile) Apply(t *testing.T, l Layout) {
	t.Helper()
	writeFile(t, filepath.Join(l.Root, m.Pkg, m.Path), m.Content, m.Exec)
}

// PkgDir creates an (empty) directory in a package.
type PkgDir struct{ Pkg, Path string }

func (m PkgDir) Apply(t *testing.T, l Layout) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(l.Root, m.Pkg, m.Path), 0o755); err != nil {
		t.Fatal(err)
	}
}

// PkgSymlink creates a symlink inside a package with the given link text.
type PkgSymlink struct{ Pkg, Path, Dest string }

func (m PkgSymlink) Apply(t *testing.T, l Layout) {
	t.Helper()
	symlink(t, m.Dest, filepath.Join(l.Root, m.Pkg, m.Path))
}

// PkgRemove deletes a path (recursively) from a package.
type PkgRemove struct{ Pkg, Path string }

func (m PkgRemove) Apply(t *testing.T, l Layout) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(l.Root, m.Pkg, m.Path)); err != nil {
		t.Fatal(err)
	}
}

// PkgIgnore writes a package's .stow-local-ignore.
type PkgIgnore struct {
	Pkg   string
	Lines []string
}

func (m PkgIgnore) Apply(t *testing.T, l Layout) {
	t.Helper()
	writeFile(t, filepath.Join(l.Root, m.Pkg, ".stow-local-ignore"), strings.Join(m.Lines, "\n")+"\n", false)
}

// TargetFile drops a regular file into the target.
type TargetFile struct{ Path, Content string }

func (m TargetFile) Apply(t *testing.T, l Layout) {
	t.Helper()
	writeFile(t, filepath.Join(l.Target, m.Path), m.Content, false)
}

// TargetDir creates a real directory in the target.
type TargetDir struct{ Path string }

func (m TargetDir) Apply(t *testing.T, l Layout) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(l.Target, m.Path), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TargetSymlink creates a symlink in the target with the given link text.
type TargetSymlink struct{ Path, Dest string }

func (m TargetSymlink) Apply(t *testing.T, l Layout) {
	t.Helper()
	symlink(t, m.Dest, filepath.Join(l.Target, m.Path))
}

// TargetRemove deletes a path from the target. A symlink is removed, not
// followed.
type TargetRemove struct{ Path string }

func (m TargetRemove) Apply(t *testing.T, l Layout) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(l.Target, m.Path)); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string, exec bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, dest, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dest, path); err != nil {
		t.Fatal(err)
	}
}

// Entry is one node of a target snapshot. Kind is "dir", "file", or "link".
// Link holds the link text; Content holds a regular file's bytes.
type Entry struct {
	Path    string
	Kind    string
	Link    string
	Content string
}

func (e Entry) String() string {
	switch e.Kind {
	case "link":
		return e.Path + " -> " + e.Link
	case "dir":
		return e.Path + "/"
	}
	return e.Path
}

// Snapshot walks the target, skipping the dotfiles root, and returns every
// entry sorted by path. Symlinks are recorded, not followed.
func Snapshot(t *testing.T, l Layout) []Entry {
	t.Helper()
	var entries []Entry
	err := filepath.WalkDir(l.Target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == l.Target {
			return nil
		}
		if path == l.Root {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(l.Target, path)
		e := Entry{Path: rel}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			e.Kind = "link"
			dest, err := os.Readlink(path)
			if err != nil {
				return err
			}
			e.Link = dest
		case d.IsDir():
			e.Kind = "dir"
		default:
			e.Kind = "file"
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			e.Content = string(b)
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

// Diff returns a human-readable difference between two snapshots, or "".
func Diff(want, got []Entry) string {
	w := map[string]Entry{}
	for _, e := range want {
		w[e.Path] = e
	}
	g := map[string]Entry{}
	for _, e := range got {
		g[e.Path] = e
	}
	var lines []string
	for _, e := range want {
		o, ok := g[e.Path]
		if !ok {
			lines = append(lines, "- "+e.String())
		} else if o != e {
			lines = append(lines, "- "+e.String(), "+ "+o.String())
		}
	}
	for _, e := range got {
		if _, ok := w[e.Path]; !ok {
			lines = append(lines, "+ "+e.String())
		}
	}
	return strings.Join(lines, "\n")
}

// StowStatus reports whether the parity suite can run: "" when stow 2.4.1
// is on PATH, else the reason to skip. Set DFM_REQUIRE_STOW=1 (CI does) to
// turn the skip into a failure.
func StowStatus() string {
	path, err := exec.LookPath("stow")
	if err != nil {
		return "stow not on PATH"
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "stow --version failed: " + err.Error()
	}
	if !strings.Contains(string(out), "version "+StowVersion) {
		return fmt.Sprintf("want stow %s, have %q", StowVersion, strings.TrimSpace(string(out)))
	}
	return ""
}

// RequireStow skips (or fails under DFM_REQUIRE_STOW) when stow 2.4.1 is
// unavailable.
func RequireStow(t *testing.T) {
	t.Helper()
	if reason := StowStatus(); reason != "" {
		if os.Getenv("DFM_REQUIRE_STOW") != "" {
			t.Fatalf("stow parity suite cannot run: %s", reason)
		}
		t.Skipf("SKIPPING stow parity suite: %s", reason)
	}
}

// RunStow runs `stow --dir=<root> --target=<target> --restow public private`
// and returns its exit code and combined output. HOME is pointed at a
// nonexistent sibling of the target so no ~/.stow-global-ignore leaks in.
// Safe to call from a goroutine: it never touches *testing.T.
func RunStow(l Layout) (int, string) {
	return runStow(l, "--restow")
}

// RunStowPlain runs `stow public private` without --restow. Used to bring
// stow's layout back in line after a step where --restow is known to die.
func RunStowPlain(l Layout) (int, string) {
	return runStow(l)
}

func runStow(l Layout, flags ...string) (int, string) {
	args := append([]string{"--dir=" + l.Root, "--target=" + l.Target}, flags...)
	cmd := exec.Command("stow", append(args, "public", "private")...)
	cmd.Env = append(os.Environ(), "HOME="+filepath.Join(filepath.Dir(l.Target), "stow-home"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(out)
		}
		return -1, err.Error()
	}
	return 0, string(out)
}

// LoadManifest reads a real-shape manifest (see internal/tools/shapegen)
// and returns the mutations that build it. Lines are
// `<pkg>\t<type>\t<exec>\t<path>[\t<link text>]` with type d, f, or l;
// `#` lines are comments. Ignore lists live beside the manifest as
// `<pkg>.stow-local-ignore`.
func LoadManifest(t *testing.T, dir string) []Mutation {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var muts []Mutation
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			t.Fatalf("bad manifest line %q", line)
		}
		pkg, typ, exec, path := fields[0], fields[1], fields[2], fields[3]
		switch typ {
		case "d":
			muts = append(muts, PkgDir{Pkg: pkg, Path: path})
		case "f":
			muts = append(muts, PkgFile{Pkg: pkg, Path: path, Exec: exec == "x"})
		case "l":
			if len(fields) != 5 {
				t.Fatalf("symlink line without target: %q", line)
			}
			muts = append(muts, PkgSymlink{Pkg: pkg, Path: path, Dest: fields[4]})
		default:
			t.Fatalf("bad manifest type %q in %q", typ, line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"public", "private"} {
		b, err := os.ReadFile(filepath.Join(dir, pkg+".stow-local-ignore"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		muts = append(muts, PkgIgnore{Pkg: pkg, Lines: strings.Split(strings.TrimRight(string(b), "\n"), "\n")})
	}
	return muts
}
