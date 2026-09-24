package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	st "github.com/tammersaleh/dotfiles-manager/internal/stow/stowtest"
)

// runIgnore drives `dfm ignore <path>` in-process against a layout.
func runIgnore(t *testing.T, l st.Layout, path string, extra ...string) (int, string, string) {
	t.Helper()
	args := append([]string{"--root", l.Root, "--target", l.Target}, extra...)
	return run(t, append(args, "ignore", path)...)
}

func readGitignore(t *testing.T, l st.Layout, pkg string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(l.Root, pkg, ".gitignore"))
	if err != nil {
		t.Fatalf("read %s/.gitignore: %v", pkg, err)
	}
	return string(b)
}

func writeGitignore(t *testing.T, l st.Layout, pkg, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(l.Root, pkg, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIgnore_Ownership covers how the owning package and the
// package-relative path are found.
func TestIgnore_Ownership(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		setup    func(t *testing.T, l st.Layout) string // returns the path argument
		wantPkg  string
		wantLine string
	}{
		{"file linked directly", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
			mustInstall(t, l)
			return ".examplerc"
		}, "public", "/.examplerc"},
		{"file under folded dir link", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".config/example/rc"}.Apply(t, l)
			st.PkgFile{Pkg: "public", Path: ".config/example/cache/blob"}.Apply(t, l)
			mustInstall(t, l)
			return ".config/example/cache/blob"
		}, "public", "/.config/example/cache/blob"},
		{"directory under folded dir link", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "private", Path: ".config/example/cache/blob"}.Apply(t, l)
			mustInstall(t, l)
			return ".config/example/cache"
		}, "private", "/.config/example/cache"},
		{"file under unfolded shared dir owned by private", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".config/alpha/rc"}.Apply(t, l)
			st.PkgFile{Pkg: "private", Path: ".config/beta/rc"}.Apply(t, l)
			st.PkgFile{Pkg: "private", Path: ".config/beta/state"}.Apply(t, l)
			mustInstall(t, l)
			return ".config/beta/state"
		}, "private", "/.config/beta/state"},
		{"path already inside root", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "private", Path: ".config/example/rc"}.Apply(t, l)
			return filepath.Join(l.Root, "private", ".config", "example", "rc")
		}, "private", "/.config/example/rc"},
		{"relative path through root", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
			return "dotfiles/public/.examplerc"
		}, "public", "/.examplerc"},
		{"absolute path in target", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
			mustInstall(t, l)
			return filepath.Join(l.Target, ".examplerc")
		}, "public", "/.examplerc"},
		{"dotted relative path", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".config/example/rc"}.Apply(t, l)
			mustInstall(t, l)
			return "./.config/../.config/example/rc"
		}, "public", "/.config/example/rc"},
		{"absolute symlink into root counts as ours", func(t *testing.T, l st.Layout) string {
			// Same rule adopt uses for already_tracked: link text inside
			// the root, absolute or relative, is dfm's.
			st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
			st.TargetSymlink{Path: ".examplerc", Dest: filepath.Join(l.Root, "public", ".examplerc")}.Apply(t, l)
			return ".examplerc"
		}, "public", "/.examplerc"},
		{"through symlinked ancestor of the target", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
			mustInstall(t, l)
			alias := filepath.Join(filepath.Dir(l.Target), "alias")
			if err := os.Symlink(l.Target, alias); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(alias, ".examplerc")
		}, "public", "/.examplerc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := st.NewLayout(t)
			arg := tt.setup(t, l)
			// Install runs before the .gitignore exists; the shape of the
			// target tree must not change as a result of ignore.
			treeBefore := st.Snapshot(t, l)

			code, out, errW := runIgnore(t, l, arg)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errW)
			}
			if out != "" {
				t.Errorf("human mode wrote to stdout: %q", out)
			}
			if !strings.Contains(errW, tt.wantLine) || !strings.Contains(errW, tt.wantPkg+"/.gitignore") {
				t.Errorf("stderr should name %s and %s/.gitignore:\n%s", tt.wantLine, tt.wantPkg, errW)
			}
			if got := readGitignore(t, l, tt.wantPkg); got != tt.wantLine+"\n" {
				t.Errorf("%s/.gitignore = %q, want %q", tt.wantPkg, got, tt.wantLine+"\n")
			}
			other := "private"
			if tt.wantPkg == "private" {
				other = "public"
			}
			if _, err := os.Lstat(filepath.Join(l.Root, other, ".gitignore")); err == nil {
				t.Errorf("%s/.gitignore was created; only the owner's should be", other)
			}
			if d := st.Diff(treeBefore, st.Snapshot(t, l)); d != "" {
				t.Errorf("target tree changed (-before +after):\n%s", d)
			}
		})
	}
}

func TestIgnore_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		setup      func(t *testing.T, l st.Layout) string
		wantErr    string
		wantDetail string // substring
	}{
		{"real file in target", func(t *testing.T, l st.Layout) string {
			st.TargetFile{Path: ".examplerc"}.Apply(t, l)
			return ".examplerc"
		}, "not_tracked", ""},
		{"real file under real dir beside a tracked sibling", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".config/alpha/rc"}.Apply(t, l)
			st.TargetFile{Path: ".config/beta/rc"}.Apply(t, l)
			mustInstall(t, l)
			return ".config/beta/rc"
		}, "not_tracked", ""},
		{"foreign symlink", func(t *testing.T, l st.Layout) string {
			st.TargetFile{Path: ".real"}.Apply(t, l)
			st.TargetSymlink{Path: ".examplerc", Dest: ".real"}.Apply(t, l)
			return ".examplerc"
		}, "not_tracked", ""},
		{"package dir itself", func(t *testing.T, l st.Layout) string {
			return filepath.Join(l.Root, "public")
		}, "not_tracked", ""},
		{"root itself", func(t *testing.T, l st.Layout) string {
			return "dotfiles"
		}, "not_tracked", ""},
		{"target itself", func(t *testing.T, l st.Layout) string {
			return "."
		}, "outside_target", ""},
		{"outside target", func(t *testing.T, l st.Layout) string {
			p := filepath.Join(filepath.Dir(l.Target), "elsewhere")
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}, "outside_target", ""},
		{"relative escaping target", func(t *testing.T, l st.Layout) string {
			return "../elsewhere"
		}, "outside_target", ""},
		{"not found", func(t *testing.T, l st.Layout) string {
			return ".missing"
		}, "not_found", ""},
		{"not found under missing parent", func(t *testing.T, l st.Layout) string {
			return ".missing/child"
		}, "not_found", ""},
		{"not found under folded dir link", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".config/example/rc"}.Apply(t, l)
			mustInstall(t, l)
			return ".config/example/missing"
		}, "not_found", "public"},
		{"not found inside root", func(t *testing.T, l st.Layout) string {
			return filepath.Join(l.Root, "public", ".missing")
		}, "not_found", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := st.NewLayout(t)
			arg := tt.setup(t, l)
			before := hashTree(t, filepath.Dir(l.Target))
			code, out, errW := runIgnore(t, l, arg)
			m := wantError(t, code, 1, out, errW, tt.wantErr)
			if d, _ := m["detail"].(string); tt.wantDetail != "" && !strings.Contains(d, tt.wantDetail) {
				t.Errorf("detail %q should contain %q", d, tt.wantDetail)
			}
			if before != hashTree(t, filepath.Dir(l.Target)) {
				t.Errorf("failed ignore changed the tree")
			}
		})
	}
}

func TestIgnore_GitignoreEditing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		before  *string // nil: no .gitignore
		want    string
		wantMsg string
	}{
		{"created when absent", nil, "/.examplerc\n", "adding"},
		{"appended after trailing newline", ptr("*.log\n"), "*.log\n/.examplerc\n", "adding"},
		{"newline inserted before append", ptr("*.log"), "*.log\n/.examplerc\n", "adding"},
		{"appended to empty file", ptr(""), "/.examplerc\n", "adding"},
		{"already present", ptr("*.log\n/.examplerc\n"), "*.log\n/.examplerc\n", "already ignored"},
		{"already present with trailing spaces", ptr("/.examplerc  \n*.log\n"), "/.examplerc  \n*.log\n", "already ignored"},
		{"already present without trailing newline", ptr("/.examplerc"), "/.examplerc", "already ignored"},
		{"unanchored line is not a match", ptr(".examplerc\n"), ".examplerc\n/.examplerc\n", "adding"},
		{"commented line is not a match", ptr("#/.examplerc\n"), "#/.examplerc\n/.examplerc\n", "adding"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := st.NewLayout(t)
			st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
			mustInstall(t, l)
			if tt.before != nil {
				writeGitignore(t, l, "public", *tt.before)
			}
			code, out, errW := runIgnore(t, l, ".examplerc")
			if code != 0 || out != "" {
				t.Fatalf("exit %d stdout %q stderr %s", code, out, errW)
			}
			if !strings.Contains(errW, tt.wantMsg) {
				t.Errorf("stderr missing %q:\n%s", tt.wantMsg, errW)
			}
			if got := readGitignore(t, l, "public"); got != tt.want {
				t.Errorf(".gitignore = %q, want %q", got, tt.want)
			}
			// Idempotent: a second run is a no-op and leaves the bytes alone.
			code, _, errW = runIgnore(t, l, ".examplerc")
			if code != 0 || !strings.Contains(errW, "already ignored") {
				t.Errorf("second run: exit %d stderr %s", code, errW)
			}
			if got := readGitignore(t, l, "public"); got != tt.want {
				t.Errorf("second run changed .gitignore to %q", got)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestIgnore_DryRun(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
	mustInstall(t, l)
	writeGitignore(t, l, "public", "*.log")
	before := hashTree(t, filepath.Dir(l.Target))

	code, out, errW := runIgnore(t, l, ".examplerc", "--dry-run")
	if code != 0 || out != "" {
		t.Fatalf("exit %d stdout %q stderr %s", code, out, errW)
	}
	for _, want := range []string{"/.examplerc", "public/.gitignore", "dry run", "nothing changed"} {
		if !strings.Contains(errW, want) {
			t.Errorf("stderr missing %q:\n%s", want, errW)
		}
	}
	if before != hashTree(t, filepath.Dir(l.Target)) {
		t.Errorf("--dry-run changed the tree")
	}

	code, out, errW = runIgnore(t, l, ".examplerc", "--dry-run", "--json")
	if code != 0 || errW != "" {
		t.Fatalf("exit %d stderr %q", code, errW)
	}
	rows, meta := jsonRows(t, out)
	if len(rows) != 1 || rows[0]["action"] != "ignore" {
		t.Errorf("dry-run rows = %v, want the ignore row alone", rows)
	}
	if meta["ignored"] != 1.0 {
		t.Errorf("_meta = %v", meta)
	}
	if before != hashTree(t, filepath.Dir(l.Target)) {
		t.Errorf("--dry-run --json changed the tree")
	}

	// A missing .gitignore is not created under --dry-run either.
	if err := os.Remove(filepath.Join(l.Root, "public", ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if code, _, errW := runIgnore(t, l, ".examplerc", "--dry-run"); code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	if _, err := os.Lstat(filepath.Join(l.Root, "public", ".gitignore")); err == nil {
		t.Errorf("--dry-run created .gitignore")
	}
}

func TestIgnore_JSONShape(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.PkgFile{Pkg: "private", Path: ".config/example/rc"}.Apply(t, l)
	st.PkgFile{Pkg: "private", Path: ".config/example/cache/blob"}.Apply(t, l)
	mustInstall(t, l)

	checkRows := func(t *testing.T, out string, want []map[string]any, wantMeta map[string]any, omitted []string) {
		t.Helper()
		rows, meta := jsonRows(t, out)
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
		for k, v := range wantMeta {
			if meta[k] != v {
				t.Errorf("_meta.%s = %v, want %v", k, meta[k], v)
			}
		}
		for _, k := range omitted {
			if _, ok := meta[k]; ok {
				t.Errorf("_meta.%s should be omitted: %v", k, meta)
			}
		}
	}

	code, out, errW := runIgnore(t, l, ".config/example/cache", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	if errW != "" {
		t.Errorf("--json must not write progress to stderr, got %q", errW)
	}
	checkRows(t, out, []map[string]any{{
		"action": "ignore", "package": "private", "path": ".config/example/cache",
		"file": "private/.gitignore", "line": "/.config/example/cache",
	}}, map[string]any{"has_more": false, "ignored": 1.0}, []string{"created", "adopted"})

	code, out, errW = runIgnore(t, l, ".config/example/cache", "--json")
	if code != 0 || errW != "" {
		t.Fatalf("exit %d stderr %q", code, errW)
	}
	checkRows(t, out, []map[string]any{{
		"action": "noop", "package": "private", "path": ".config/example/cache",
		"file": "private/.gitignore", "line": "/.config/example/cache", "reason": "already_ignored",
	}}, map[string]any{"has_more": false}, []string{"ignored", "created", "adopted"})
}

func TestIgnore_Verbose(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
	mustInstall(t, l)
	code, _, errW := runIgnore(t, l, ".examplerc", "--verbose")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	if !strings.Contains(errW, filepath.Join(l.Root, "public", ".gitignore")) {
		t.Errorf("--verbose should name the file written:\n%s", errW)
	}
}
