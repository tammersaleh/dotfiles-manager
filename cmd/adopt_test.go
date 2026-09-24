package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	st "github.com/tammersaleh/dotfiles-manager/internal/stow/stowtest"
)

// runAdopt drives `dfm <pkg> <path>` in-process against a layout.
func runAdopt(t *testing.T, l st.Layout, pkg, path string, extra ...string) (int, string, string) {
	t.Helper()
	args := append([]string{"--root", l.Root, "--target", l.Target}, extra...)
	return run(t, append(args, pkg, path)...)
}

// wantError asserts one fatal JSON error on stderr with the given code and
// returns it.
func wantError(t *testing.T, code int, wantCode int, out, errW, wantErr string) map[string]any {
	t.Helper()
	if code != wantCode {
		t.Fatalf("exit %d, want %d\nstdout %s\nstderr %s", code, wantCode, out, errW)
	}
	if out != "" {
		t.Errorf("stdout should be empty on error, got %q", out)
	}
	m := oneJSONObject(t, errW)
	if m["error"] != wantErr {
		t.Errorf("error = %v, want %s (%v)", m["error"], wantErr, m)
	}
	for _, k := range []string{"detail", "hint", "path"} {
		if v, _ := m[k].(string); v == "" {
			t.Errorf("error missing %s: %v", k, m)
		}
	}
	return m
}

func TestAdopt_File(t *testing.T) {
	t.Parallel()
	for _, pkg := range []string{"public", "private"} {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()
			l := st.NewLayout(t)
			st.PkgFile{Pkg: "public", Path: ".other"}.Apply(t, l)
			mustInstall(t, l)
			st.TargetFile{Path: ".examplerc", Content: "mine"}.Apply(t, l)

			code, out, errW := runAdopt(t, l, pkg, ".examplerc")
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errW)
			}
			if out != "" {
				t.Errorf("human mode wrote to stdout: %q", out)
			}
			for _, want := range []string{"moving .examplerc to " + pkg, "LINK .examplerc -> dotfiles/" + pkg + "/.examplerc", "created 1, removed 0"} {
				if !strings.Contains(errW, want) {
					t.Errorf("stderr missing %q:\n%s", want, errW)
				}
			}
			want := []st.Entry{
				{Path: ".examplerc", Kind: "link", Link: "dotfiles/" + pkg + "/.examplerc"},
				{Path: ".other", Kind: "link", Link: "dotfiles/public/.other"},
			}
			if d := st.Diff(want, st.Snapshot(t, l)); d != "" {
				t.Errorf("tree (-want +got):\n%s", d)
			}
			b, err := os.ReadFile(filepath.Join(l.Root, pkg, ".examplerc"))
			if err != nil || string(b) != "mine" {
				t.Errorf("package file: %q, %v", b, err)
			}
			// Idempotent: a second install does nothing.
			if code, _, errW := runInstall(t, l, "--json"); code != 0 || strings.Contains(errW, "error") {
				t.Errorf("install after adopt: exit %d %s", code, errW)
			}
		})
	}
}

func TestAdopt_Directory(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.TargetFile{Path: ".config/example/config.toml", Content: "a"}.Apply(t, l)
	st.TargetFile{Path: ".config/example/sub/deep", Content: "b"}.Apply(t, l)

	code, _, errW := runAdopt(t, l, "public", ".config/example")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	// .config is a real dir in the target; the adopted dir folds.
	want := []st.Entry{
		{Path: ".config", Kind: "dir"},
		{Path: ".config/example", Kind: "link", Link: "../dotfiles/public/.config/example"},
	}
	if d := st.Diff(want, st.Snapshot(t, l)); d != "" {
		t.Errorf("tree (-want +got):\n%s", d)
	}
	for path, content := range map[string]string{".config/example/config.toml": "a", ".config/example/sub/deep": "b"} {
		b, err := os.ReadFile(filepath.Join(l.Root, "public", path))
		if err != nil || string(b) != content {
			t.Errorf("%s: %q, %v", path, b, err)
		}
	}
}

func TestAdopt_PathForms(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.TargetFile{Path: ".a"}.Apply(t, l)
	st.TargetFile{Path: ".dir/b"}.Apply(t, l)
	st.TargetFile{Path: ".c"}.Apply(t, l)

	// Absolute.
	if code, _, errW := runAdopt(t, l, "public", filepath.Join(l.Target, ".a")); code != 0 {
		t.Fatalf("absolute: exit %d: %s", code, errW)
	}
	// Relative with a dot segment.
	if code, _, errW := runAdopt(t, l, "public", "./.dir/../.dir/b"); code != 0 {
		t.Fatalf("relative dotted: exit %d: %s", code, errW)
	}
	// Absolute through a symlinked ancestor: <tmp>/alias -> <tmp>/home.
	alias := filepath.Join(filepath.Dir(l.Target), "alias")
	if err := os.Symlink(l.Target, alias); err != nil {
		t.Fatal(err)
	}
	if code, _, errW := runAdopt(t, l, "private", filepath.Join(alias, ".c")); code != 0 {
		t.Fatalf("via symlinked ancestor: exit %d: %s", code, errW)
	}
	want := []st.Entry{
		{Path: ".a", Kind: "link", Link: "dotfiles/public/.a"},
		{Path: ".c", Kind: "link", Link: "dotfiles/private/.c"},
		{Path: ".dir", Kind: "dir"},
		{Path: ".dir/b", Kind: "link", Link: "../dotfiles/public/.dir/b"},
	}
	if d := st.Diff(want, st.Snapshot(t, l)); d != "" {
		t.Errorf("tree (-want +got):\n%s", d)
	}
}

func TestAdopt_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		setup      func(t *testing.T, l st.Layout) string // returns the path argument
		wantCode   int
		wantErr    string
		wantHint   string // substring
		wantDetail string // substring
	}{
		{"outside target", func(t *testing.T, l st.Layout) string {
			p := filepath.Join(filepath.Dir(l.Target), "elsewhere")
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}, 1, "outside_target", "", ""},
		{"relative escaping target", func(t *testing.T, l st.Layout) string {
			return "../elsewhere"
		}, 1, "outside_target", "", ""},
		{"target itself", func(t *testing.T, l st.Layout) string {
			return "."
		}, 1, "outside_target", "", ""},
		{"inside root", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
			return "dotfiles/public/.examplerc"
		}, 1, "inside_root", "", ""},
		{"root itself", func(t *testing.T, l st.Layout) string {
			return "dotfiles"
		}, 1, "inside_root", "", ""},
		{"through folded dir link", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".config/example/rc"}.Apply(t, l)
			mustInstall(t, l)
			return ".config/example/rc"
		}, 1, "inside_root", "public", ""},
		{"not found", func(t *testing.T, l st.Layout) string {
			return ".missing"
		}, 1, "not_found", "", ""},
		{"not found under missing parent", func(t *testing.T, l st.Layout) string {
			return ".missing/child"
		}, 1, "not_found", "", ""},
		{"already tracked in package", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
			st.TargetFile{Path: ".examplerc", Content: "mine"}.Apply(t, l)
			return ".examplerc"
		}, 1, "already_tracked", "public", ""},
		{"already tracked link into other package", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "private", Path: ".examplerc"}.Apply(t, l)
			mustInstall(t, l)
			return ".examplerc"
		}, 1, "already_tracked", "private", ""},
		{"already tracked link into same package", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "public", Path: ".examplerc"}.Apply(t, l)
			mustInstall(t, l)
			return ".examplerc"
		}, 1, "already_tracked", "public", ""},
		{"directory holding tracked links", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "private", Path: ".config/alpha/rc"}.Apply(t, l)
			st.TargetFile{Path: ".config/gamma/rc"}.Apply(t, l)
			mustInstall(t, l)
			return ".config"
		}, 1, "contains_tracked", "", ".config/alpha"},
		{"pre-existing conflict elsewhere", func(t *testing.T, l st.Layout) string {
			st.PkgFile{Pkg: "private", Path: ".clash"}.Apply(t, l)
			st.TargetFile{Path: ".clash", Content: "theirs"}.Apply(t, l)
			st.TargetFile{Path: ".examplerc"}.Apply(t, l)
			return ".examplerc"
		}, 2, "conflict", "dfm private .clash", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := st.NewLayout(t)
			arg := tt.setup(t, l)
			before := hashTree(t, filepath.Dir(l.Target))
			code, out, errW := runAdopt(t, l, "public", arg)
			m := wantError(t, code, tt.wantCode, out, errW, tt.wantErr)
			if h, _ := m["hint"].(string); !strings.Contains(h, tt.wantHint) {
				t.Errorf("hint %q should contain %q", h, tt.wantHint)
			}
			if d, _ := m["detail"].(string); tt.wantDetail != "" && !strings.Contains(d, tt.wantDetail) {
				t.Errorf("detail %q should contain %q", d, tt.wantDetail)
			}
			if before != hashTree(t, filepath.Dir(l.Target)) {
				t.Errorf("failed adopt changed the tree")
			}
		})
	}
}

func TestAdopt_DryRun(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.TargetFile{Path: ".examplerc", Content: "mine"}.Apply(t, l)
	st.PkgFile{Pkg: "public", Path: ".pending"}.Apply(t, l)
	before := hashTree(t, filepath.Dir(l.Target))

	code, out, errW := runAdopt(t, l, "public", ".examplerc", "--dry-run")
	if code != 0 || out != "" {
		t.Fatalf("exit %d stdout %q stderr %s", code, out, errW)
	}
	for _, want := range []string{"moving .examplerc to public", "dry run", "nothing changed"} {
		if !strings.Contains(errW, want) {
			t.Errorf("stderr missing %q:\n%s", want, errW)
		}
	}
	if before != hashTree(t, filepath.Dir(l.Target)) {
		t.Errorf("--dry-run changed the tree")
	}

	code, out, errW = runAdopt(t, l, "public", ".examplerc", "--dry-run", "--json")
	if code != 0 || errW != "" {
		t.Fatalf("exit %d stderr %q", code, errW)
	}
	rows, meta := jsonRows(t, out)
	if len(rows) != 1 || rows[0]["action"] != "adopt" {
		t.Errorf("dry-run rows = %v, want the adopt row alone", rows)
	}
	if meta["adopted"] != 1.0 {
		t.Errorf("_meta = %v", meta)
	}
	if before != hashTree(t, filepath.Dir(l.Target)) {
		t.Errorf("--dry-run --json changed the tree")
	}
}

func TestAdopt_JSONShape(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.TargetFile{Path: ".config/example/rc"}.Apply(t, l)

	code, out, errW := runAdopt(t, l, "private", ".config/example", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	if errW != "" {
		t.Errorf("--json must not write progress to stderr, got %q", errW)
	}
	rows, meta := jsonRows(t, out)
	want := []map[string]any{
		{"action": "adopt", "package": "private", "path": ".config/example", "from": ".config/example", "to": "private/.config/example"},
		{"action": "link", "package": "private", "path": ".config/example", "target": "../dotfiles/private/.config/example"},
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
	wantMeta := map[string]any{"has_more": false, "adopted": 1.0, "created": 1.0}
	for k, v := range wantMeta {
		if meta[k] != v {
			t.Errorf("_meta.%s = %v, want %v", k, meta[k], v)
		}
	}
	for _, k := range []string{"removed", "unfolded", "refolded"} {
		if _, ok := meta[k]; ok {
			t.Errorf("_meta.%s should be omitted when zero: %v", k, meta)
		}
	}
}

func TestAdopt_IntoSharedRealDir(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.PkgFile{Pkg: "public", Path: ".config/alpha/rc"}.Apply(t, l)
	mustInstall(t, l)
	// .config is a folded link to public, so the new dir must live
	// beside it under a different top-level name for the parent to be real.
	st.TargetFile{Path: ".local/beta/rc"}.Apply(t, l)
	st.PkgFile{Pkg: "public", Path: ".local/gamma/rc"}.Apply(t, l)
	mustInstall(t, l)
	// Now .local is a real dir: public's gamma is linked into it because
	// beta already occupied the directory.
	code, _, errW := runAdopt(t, l, "private", ".local/beta", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	want := []st.Entry{
		{Path: ".config", Kind: "link", Link: "dotfiles/public/.config"},
		{Path: ".local", Kind: "dir"},
		{Path: ".local/beta", Kind: "link", Link: "../dotfiles/private/.local/beta"},
		{Path: ".local/gamma", Kind: "link", Link: "../dotfiles/public/.local/gamma"},
	}
	if d := st.Diff(want, st.Snapshot(t, l)); d != "" {
		t.Errorf("tree (-want +got):\n%s", d)
	}
}

// A file adopted into private under a directory that public has folded
// into a single link cannot be reached (the path resolves inside the
// root), so the unfold case is: public folded .config, and a new real file
// sits in a sibling real dir. Covered above. This case: the moved file's
// parent exists in both packages after the move, so install unfolds.
func TestAdopt_NewOverlapUnfolds(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.PkgFile{Pkg: "public", Path: ".config/alpha/rc"}.Apply(t, l)
	st.TargetFile{Path: ".config/beta/rc"}.Apply(t, l)
	// Install conflicts? No: .config is a real dir, public links alpha in.
	mustInstall(t, l)

	code, out, errW := runAdopt(t, l, "private", ".config/beta", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	rows, _ := jsonRows(t, out)
	kinds := make([]string, 0, len(rows))
	for _, r := range rows {
		kinds = append(kinds, r["action"].(string))
	}
	if got := strings.Join(kinds, ","); got != "adopt,link" {
		t.Errorf("actions = %s, want adopt,link", got)
	}
	want := []st.Entry{
		{Path: ".config", Kind: "dir"},
		{Path: ".config/alpha", Kind: "link", Link: "../dotfiles/public/.config/alpha"},
		{Path: ".config/beta", Kind: "link", Link: "../dotfiles/private/.config/beta"},
	}
	if d := st.Diff(want, st.Snapshot(t, l)); d != "" {
		t.Errorf("tree (-want +got):\n%s", d)
	}
}

func TestAdopt_SymlinkSourceMovedAsLink(t *testing.T) {
	t.Parallel()
	l := st.NewLayout(t)
	st.TargetFile{Path: ".config/example/init.lua"}.Apply(t, l)
	st.TargetSymlink{Path: ".example", Dest: ".config/example"}.Apply(t, l)

	code, _, errW := runAdopt(t, l, "public", ".example")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	dest, err := os.Readlink(filepath.Join(l.Root, "public", ".example"))
	if err != nil || dest != ".config/example" {
		t.Errorf("package symlink = %q, %v; want the original link text", dest, err)
	}
	want := []st.Entry{
		{Path: ".config", Kind: "dir"},
		{Path: ".config/example", Kind: "dir"},
		{Path: ".config/example/init.lua", Kind: "file"},
		{Path: ".example", Kind: "link", Link: "dotfiles/public/.example"},
	}
	if d := st.Diff(want, st.Snapshot(t, l)); d != "" {
		t.Errorf("tree (-want +got):\n%s", d)
	}
}
