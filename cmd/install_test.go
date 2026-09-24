package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	st "github.com/tammersaleh/dotfiles-manager/internal/stow/stowtest"
)

// installFixtures are layer 2 behavioral fixtures: hand-written
// expectations for exit codes, action counts, error codes, and the
// resulting tree. Every Run drives cmd.Run in-process.
var installFixtures = []st.Fixture{
	{Name: "single package folds", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgFile{Pkg: "public", Path: ".example/rc"},
		st.PkgFile{Pkg: "public", Path: ".example/sub/deep"},
		st.Run{WantActions: 2, WantTree: []st.Entry{
			{Path: ".example", Kind: "link", Link: "dotfiles/public/.example"},
			{Path: ".examplerc", Kind: "link", Link: "dotfiles/public/.examplerc"},
		}},
		st.Run{WantActions: 0},
	}},
	{Name: "two packages unfold one dir", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "public", Path: ".zshenv"},
		st.Run{WantActions: 2},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		// unlink .config, mkdir .config, unfold marker, link alpha, link beta
		st.Run{WantActions: 5, WantTree: []st.Entry{
			{Path: ".config", Kind: "dir"},
			{Path: ".config/alpha", Kind: "link", Link: "../dotfiles/public/.config/alpha"},
			{Path: ".config/beta", Kind: "link", Link: "../dotfiles/private/.config/beta"},
			{Path: ".zshenv", Kind: "link", Link: "dotfiles/public/.zshenv"},
		}},
		st.Run{WantActions: 0},
	}},
	{Name: "deep unfold gives long relative links", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/a/b/c/d"},
		st.PkgFile{Pkg: "private", Path: ".config/a/b/c/e"},
		// four (mkdir, unfold) pairs and two links
		st.Run{WantActions: 10, WantTree: []st.Entry{
			{Path: ".config", Kind: "dir"},
			{Path: ".config/a", Kind: "dir"},
			{Path: ".config/a/b", Kind: "dir"},
			{Path: ".config/a/b/c", Kind: "dir"},
			{Path: ".config/a/b/c/d", Kind: "link", Link: "../../../../dotfiles/public/.config/a/b/c/d"},
			{Path: ".config/a/b/c/e", Kind: "link", Link: "../../../../dotfiles/private/.config/a/b/c/e"},
		}},
	}},
	{Name: "second owner file deleted leaves dir unfolded", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		st.Run{WantActions: 4},
		st.PkgRemove{Pkg: "private", Path: ".config/beta"},
		// Stow never refolds in the stow phase: the dir stays real.
		st.Run{WantActions: 1, WantTree: []st.Entry{
			{Path: ".config", Kind: "dir"},
			{Path: ".config/alpha", Kind: "link", Link: "../dotfiles/public/.config/alpha"},
		}},
	}},
	{Name: "symlink inside package linked as file", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/example/init.lua"},
		st.PkgSymlink{Pkg: "public", Path: ".example", Dest: ".config/example"},
		st.Run{WantActions: 2, WantTree: []st.Entry{
			{Path: ".config", Kind: "link", Link: "dotfiles/public/.config"},
			{Path: ".example", Kind: "link", Link: "dotfiles/public/.example"},
		}},
	}},
	{Name: "ignore list present", Steps: []st.Step{
		st.PkgIgnore{Pkg: "public", Lines: []string{`\.gitignore`, `README\.md`, `\.claude/settings\.local\.json`}},
		st.PkgFile{Pkg: "public", Path: ".gitignore"},
		st.PkgFile{Pkg: "public", Path: "README.md"},
		st.PkgFile{Pkg: "public", Path: "LICENSE"},
		st.PkgFile{Pkg: "public", Path: ".claude/settings.local.json"},
		st.PkgFile{Pkg: "public", Path: ".claude/settings.json"},
		st.PkgFile{Pkg: "private", Path: ".claude/other.json"},
		// link LICENSE, mkdir .claude, unfold .claude, two links
		st.Run{WantActions: 5, WantTree: []st.Entry{
			{Path: ".claude", Kind: "dir"},
			{Path: ".claude/other.json", Kind: "link", Link: "../dotfiles/private/.claude/other.json"},
			{Path: ".claude/settings.json", Kind: "link", Link: "../dotfiles/public/.claude/settings.json"},
			{Path: "LICENSE", Kind: "link", Link: "dotfiles/public/LICENSE"},
		}},
	}},
	{Name: "ignore list absent uses defaults", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".gitignore"},
		st.PkgFile{Pkg: "public", Path: "README.md"},
		st.PkgFile{Pkg: "public", Path: "LICENSE"},
		st.PkgFile{Pkg: "public", Path: ".example/README.md"},
		st.PkgFile{Pkg: "public", Path: ".example/backup~"},
		st.PkgFile{Pkg: "public", Path: ".example/rc"},
		st.Run{WantActions: 1, WantTree: []st.Entry{
			{Path: ".example", Kind: "link", Link: "dotfiles/public/.example"},
		}},
	}},
	{Name: "conflicts are all reported and nothing changes", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgFile{Pkg: "public", Path: ".other"},
		st.PkgFile{Pkg: "private", Path: ".third"},
		st.TargetFile{Path: ".examplerc", Content: "mine"},
		st.TargetFile{Path: ".third", Content: "also mine"},
		st.Run{WantExit: 2, WantErrors: []string{"conflict", "conflict"}, WantTree: []st.Entry{
			{Path: ".examplerc", Kind: "file", Content: "mine"},
			{Path: ".third", Kind: "file", Content: "also mine"},
		}},
		st.TargetRemove{Path: ".examplerc"},
		st.TargetRemove{Path: ".third"},
		st.Run{WantActions: 3},
	}},
	{Name: "foreign symlink conflicts", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.TargetSymlink{Path: ".examplerc", Dest: "/etc/hosts"},
		st.Run{WantExit: 2, WantErrors: []string{"conflict"}, WantTree: []st.Entry{
			{Path: ".examplerc", Kind: "link", Link: "/etc/hosts"},
		}},
	}},
	{Name: "directory where file wanted conflicts", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.TargetDir{Path: ".examplerc"},
		st.Run{WantExit: 2, WantErrors: []string{"conflict"}, WantTree: []st.Entry{
			{Path: ".examplerc", Kind: "dir"},
		}},
	}},
	{Name: "same path in both packages conflicts", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgFile{Pkg: "private", Path: ".examplerc"},
		st.Run{WantExit: 2, WantErrors: []string{"conflict"}, WantTree: []st.Entry{}},
	}},
	{Name: "absolute symlink in package conflicts", Steps: []st.Step{
		st.PkgSymlink{Pkg: "public", Path: ".hosts", Dest: "/etc/hosts"},
		st.Run{WantExit: 2, WantErrors: []string{"conflict"}, WantTree: []st.Entry{}},
	}},
	{Name: "dangling link removed after package file deleted", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgFile{Pkg: "public", Path: ".other"},
		st.Run{WantActions: 2},
		st.PkgRemove{Pkg: "public", Path: ".examplerc"},
		st.Run{WantActions: 1, WantTree: []st.Entry{
			{Path: ".other", Kind: "link", Link: "dotfiles/public/.other"},
		}},
	}},
	{Name: "bad ignore pattern is fatal", Steps: []st.Step{
		st.PkgIgnore{Pkg: "private", Lines: []string{`\.gitignore`, `(unclosed`}},
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.Run{WantExit: 1, WantErrors: []string{"bad_ignore_pattern"}, WantTree: []st.Entry{}},
	}},
}

// runInstall drives `dfm install` in-process against a layout.
func runInstall(t *testing.T, l st.Layout, extra ...string) (int, string, string) {
	t.Helper()
	args := append([]string{"--root", l.Root, "--target", l.Target}, extra...)
	return run(t, append(args, "install")...)
}

// stderrErrors decodes every line of stderr as a JSON error object and
// returns the error codes in order. Fails on any non-JSON line.
func stderrErrors(t *testing.T, stderr string) []string {
	t.Helper()
	var codes []string
	for _, line := range strings.Split(strings.TrimRight(stderr, "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stderr line is not JSON: %q", line)
		}
		code, _ := m["error"].(string)
		codes = append(codes, code)
		for _, k := range []string{"detail", "hint", "path"} {
			if v, _ := m[k].(string); v == "" {
				t.Errorf("error line missing %s: %s", k, line)
			}
		}
	}
	return codes
}

// jsonRows decodes stdout as JSONL and returns the action rows and _meta.
func jsonRows(t *testing.T, stdout string) ([]map[string]any, map[string]any) {
	t.Helper()
	var rows []map[string]any
	var meta map[string]any
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stdout line %d not JSON: %q", i, line)
		}
		if mm, ok := m["_meta"].(map[string]any); ok {
			if i != len(lines)-1 {
				t.Fatalf("_meta is not the last line: %q", stdout)
			}
			meta = mm
			continue
		}
		rows = append(rows, m)
	}
	if meta == nil {
		t.Fatalf("no _meta trailer in %q", stdout)
	}
	return rows, meta
}

func TestInstall_Fixtures(t *testing.T) {
	for _, fx := range installFixtures {
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()
			l := st.NewLayout(t)
			for i, step := range fx.Steps {
				switch s := step.(type) {
				case st.Mutation:
					s.Apply(t, l)
				case st.Run:
					// Plan with --json first, without changing anything, so the
					// action count and the conflict behavior can be checked
					// against the same starting tree.
					before := st.Snapshot(t, l)
					code, out, errW := runInstall(t, l, "--json", "--dry-run")
					if code != s.WantExit {
						t.Fatalf("step %d: dry-run exit %d, want %d\nstdout %s\nstderr %s", i, code, s.WantExit, out, errW)
					}
					if d := st.Diff(before, st.Snapshot(t, l)); d != "" {
						t.Fatalf("step %d: --dry-run changed the tree:\n%s", i, d)
					}
					if s.WantExit != 0 {
						if out != "" {
							t.Errorf("step %d: stdout should be empty on error, got %q", i, out)
						}
						if got := stderrErrors(t, errW); strings.Join(got, ",") != strings.Join(s.WantErrors, ",") {
							t.Errorf("step %d: errors = %v, want %v", i, got, s.WantErrors)
						}
					} else {
						rows, _ := jsonRows(t, out)
						if len(rows) != s.WantActions {
							t.Errorf("step %d: planned %d actions, want %d: %v", i, len(rows), s.WantActions, rows)
						}
					}

					// Now for real.
					code, out, errW = runInstall(t, l)
					if code != s.WantExit {
						t.Fatalf("step %d: exit %d, want %d\nstderr %s", i, code, s.WantExit, errW)
					}
					if out != "" {
						t.Errorf("step %d: human mode wrote to stdout: %q", i, out)
					}
					if s.WantExit != 0 {
						if d := st.Diff(before, st.Snapshot(t, l)); d != "" {
							t.Errorf("step %d: failed run changed the tree:\n%s", i, d)
						}
					} else if strings.Contains(errW, `"error"`) {
						t.Errorf("step %d: unexpected error on stderr: %s", i, errW)
					}
					if s.WantTree != nil {
						if d := st.Diff(s.WantTree, st.Snapshot(t, l)); d != "" {
							t.Errorf("step %d: tree (-want +got):\n%s", i, d)
						}
					}
				}
			}
		})
	}
}

func TestInstall_JSONShape(t *testing.T) {
	l := st.NewLayout(t)
	st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"}.Apply(t, l)
	st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
	st.PkgFile{Pkg: "private", Path: ".gone"}.Apply(t, l)
	if code, _, errW := runInstall(t, l); code != 0 {
		t.Fatalf("seed install failed: %s", errW)
	}
	// Force an unfold and a dangling-link removal in one run.
	st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"}.Apply(t, l)
	st.PkgRemove{Pkg: "private", Path: ".gone"}.Apply(t, l)

	code, out, errW := runInstall(t, l, "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	if errW != "" {
		t.Errorf("--json must not write progress to stderr, got %q", errW)
	}
	rows, meta := jsonRows(t, out)

	want := []map[string]any{
		{"action": "unlink", "package": "private", "path": ".gone", "reason": "source_missing"},
		{"action": "unlink", "package": "public", "path": ".config"},
		{"action": "mkdir", "path": ".config"},
		{"action": "unfold", "package": "public", "path": ".config"},
		{"action": "link", "package": "public", "path": ".config/alpha", "target": "../dotfiles/public/.config/alpha"},
		{"action": "link", "package": "private", "path": ".config/beta", "target": "../dotfiles/private/.config/beta"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v, want %v", rows, want)
	}
	for i := range want {
		for k, v := range want[i] {
			if rows[i][k] != v {
				t.Errorf("row %d %s = %v, want %v (row %v)", i, k, rows[i][k], v, rows[i])
			}
		}
		for k := range rows[i] {
			if _, ok := want[i][k]; !ok {
				t.Errorf("row %d has unexpected key %s", i, k)
			}
		}
	}
	wantMeta := map[string]any{"has_more": false, "created": 2.0, "removed": 2.0, "unfolded": 1.0}
	for k, v := range wantMeta {
		if meta[k] != v {
			t.Errorf("_meta.%s = %v, want %v", k, meta[k], v)
		}
	}
	if _, ok := meta["refolded"]; ok {
		t.Errorf("_meta.refolded should be omitted when zero: %v", meta)
	}
}

func TestInstall_HumanOutput(t *testing.T) {
	l := st.NewLayout(t)
	st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)

	code, out, errW := runInstall(t, l, "--dry-run")
	if code != 0 || out != "" {
		t.Fatalf("exit %d stdout %q", code, out)
	}
	for _, want := range []string{"LINK .examplerc -> dotfiles/public/.examplerc", "dry run", "nothing changed"} {
		if !strings.Contains(errW, want) {
			t.Errorf("dry-run stderr missing %q:\n%s", want, errW)
		}
	}
	if _, err := os.Lstat(filepath.Join(l.Target, ".examplerc")); !os.IsNotExist(err) {
		t.Errorf("--dry-run created the link")
	}

	code, _, errW = runInstall(t, l)
	if code != 0 || !strings.Contains(errW, "created 1, removed 0") {
		t.Errorf("exit %d stderr %q", code, errW)
	}

	code, _, errW = runInstall(t, l, "--quiet")
	if code != 0 || errW != "" {
		t.Errorf("--quiet: exit %d stderr %q", code, errW)
	}

	code, _, errW = runInstall(t, l)
	if code != 0 || !strings.Contains(errW, "nothing to do") {
		t.Errorf("idempotent run: exit %d stderr %q", code, errW)
	}
}

func TestInstall_Verbose(t *testing.T) {
	l := st.NewLayout(t)
	st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
	code, _, errW := runInstall(t, l, "--verbose", "--quiet")
	if code != 0 {
		t.Fatal(errW)
	}
	if !strings.Contains(errW, "symlink "+filepath.Join(l.Target, ".examplerc")) {
		t.Errorf("--verbose should echo the symlink call even under --quiet:\n%s", errW)
	}
}

func TestInstall_ConflictHint(t *testing.T) {
	l := st.NewLayout(t)
	st.PkgFile{Pkg: "private", Path: ".examplerc"}.Apply(t, l)
	st.TargetFile{Path: ".examplerc", Content: "x"}.Apply(t, l)
	code, _, errW := runInstall(t, l)
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	m := oneJSONObject(t, errW)
	if m["error"] != "conflict" || m["path"] != ".examplerc" {
		t.Errorf("got %v", m)
	}
	if h, _ := m["hint"].(string); !strings.Contains(h, "dfm private .examplerc") {
		t.Errorf("hint should name the owning package: %q", h)
	}
}

func TestInstall_PackageMissing(t *testing.T) {
	l := st.NewLayout(t)
	if err := os.RemoveAll(filepath.Join(l.Root, "private")); err != nil {
		t.Fatal(err)
	}
	code, _, errW := runInstall(t, l)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	if m := oneJSONObject(t, errW); m["error"] != "package_missing" {
		t.Errorf("got %v", m)
	}
}

func TestInstall_BadIgnorePatternNamesFileAndLine(t *testing.T) {
	l := st.NewLayout(t)
	st.PkgIgnore{Pkg: "public", Lines: []string{"ok", "", "# c", `(bad`}}.Apply(t, l)
	code, _, errW := runInstall(t, l)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	m := oneJSONObject(t, errW)
	if m["error"] != "bad_ignore_pattern" {
		t.Fatalf("got %v", m)
	}
	wantFile := filepath.Join(l.Root, "public", ".stow-local-ignore")
	if m["path"] != wantFile {
		t.Errorf("path = %v, want %s", m["path"], wantFile)
	}
	if d, _ := m["detail"].(string); !strings.Contains(d, wantFile+":4:") {
		t.Errorf("detail should name file:line, got %q", d)
	}
}

func TestInstall_RealShape(t *testing.T) {
	l := st.NewLayout(t)
	for _, m := range st.LoadManifest(t, filepath.Join("..", "testdata", "real-shape")) {
		m.Apply(t, l)
	}
	code, out, errW := runInstall(t, l, "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	rows, _ := jsonRows(t, out)
	if len(rows) == 0 {
		t.Fatal("real shape planned nothing")
	}
	code, out, errW = runInstall(t, l, "--json")
	if code != 0 {
		t.Fatalf("second run exit %d: %s", code, errW)
	}
	if rows, _ := jsonRows(t, out); len(rows) != 0 {
		t.Errorf("second run planned %d actions, want 0: %v", len(rows), rows)
	}
}
