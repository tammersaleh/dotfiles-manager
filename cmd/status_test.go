package cmd_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tammersaleh/dotfiles-manager/internal/gitx/gittest"
	st "github.com/tammersaleh/dotfiles-manager/internal/stow/stowtest"
)

// seedStatus builds a layout whose packages are pushed, in-sync clones and
// returns it with the remote path per package. Not parallel-safe: it
// isolates HOME for the duration of the test.
func seedStatus(t *testing.T) (st.Layout, map[string]string) {
	t.Helper()
	gittest.Isolate(t)
	l := st.NewLayout(t)
	remotes := gittest.Seed(t, l.Root, filepath.Join(filepath.Dir(l.Target), "remotes"))
	return l, remotes
}

func runStatus(t *testing.T, l st.Layout, extra ...string) (int, string, string) {
	t.Helper()
	args := append([]string{"--root", l.Root, "--target", l.Target}, extra...)
	return run(t, append(args, "status")...)
}

func mustInstall(t *testing.T, l st.Layout) {
	t.Helper()
	if code, _, errW := runInstall(t, l); code != 0 {
		t.Fatalf("install: exit %d: %s", code, errW)
	}
}

// hashTree hashes every path under dir, including .git directories and the
// dotfiles root, by name, type, link text, and content. Unlike
// stowtest.Snapshot it covers the packages too, so it proves status does not
// touch the repositories either.
func hashTree(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		h.Write([]byte(rel + "\x00" + d.Type().String() + "\x00"))
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			dest, err := os.Readlink(path)
			if err != nil {
				return err
			}
			h.Write([]byte(dest))
		case !d.IsDir():
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			h.Write(b)
		}
		h.Write([]byte{0})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestStatus_CleanInSync(t *testing.T) {
	l, _ := seedStatus(t)
	mustInstall(t, l)

	code, out, errW := runStatus(t, l)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	if errW != "" {
		t.Errorf("stderr should be empty, got %q", errW)
	}
	want := "public: clean, in sync with origin/main\nprivate: clean, in sync with origin/main\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestStatus_Dirty(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, l st.Layout)
		want   string // private's line
	}{
		{"modified", func(t *testing.T, l st.Layout) {
			if err := os.WriteFile(filepath.Join(l.Root, "private", ".privaterc"), []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "private: dirty (1 change), in sync with origin/main\n"},
		{"untracked only", func(t *testing.T, l st.Layout) {
			st.PkgFile{Pkg: "private", Path: ".new"}.Apply(t, l)
			st.PkgFile{Pkg: "private", Path: ".config/example/rc"}.Apply(t, l)
		}, "private: dirty (2 changes), in sync with origin/main\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, _ := seedStatus(t)
			mustInstall(t, l)
			tt.mutate(t, l)
			code, out, errW := runStatus(t, l)
			if code != 0 {
				t.Fatalf("dirty repo must not fail status: exit %d: %s", code, errW)
			}
			lines := strings.SplitAfter(out, "\n")
			if lines[0] != "public: clean, in sync with origin/main\n" {
				t.Errorf("public line = %q", lines[0])
			}
			// The untracked case adds a pending link for install; skip past it.
			if lines[1] != tt.want {
				t.Errorf("private line = %q, want %q", lines[1], tt.want)
			}
		})
	}
}

func TestStatus_AheadBehind(t *testing.T) {
	l, remotes := seedStatus(t)
	mustInstall(t, l)

	// public ahead by one.
	gittest.Commit(t, filepath.Join(l.Root, "public"), ".examplerc", "v2\n", "local change")
	// private behind by two, after a fetch: status itself never fetches.
	other := filepath.Join(t.TempDir(), "other")
	gittest.Clone(t, remotes["private"], other)
	gittest.Commit(t, other, ".privaterc", "a\n", "elsewhere 1")
	gittest.Commit(t, other, ".privaterc", "b\n", "elsewhere 2")
	gittest.Push(t, other)
	gittest.Git(t, filepath.Join(l.Root, "private"), "fetch", "--quiet")

	code, out, errW := runStatus(t, l)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errW)
	}
	want := "public: clean, ahead 1 of origin/main\nprivate: clean, behind 2 of origin/main\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}

	code, out, _ = runStatus(t, l, "--json")
	if code != 0 {
		t.Fatal(code)
	}
	rows, _ := jsonRows(t, out)
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0]["ahead"] != 1.0 || rows[0]["behind"] != 0.0 || rows[1]["ahead"] != 0.0 || rows[1]["behind"] != 2.0 {
		t.Errorf("ahead/behind rows = %v", rows)
	}
}

func TestStatus_NoUpstream(t *testing.T) {
	l, _ := seedStatus(t)
	mustInstall(t, l)
	gittest.Git(t, filepath.Join(l.Root, "private"), "branch", "--unset-upstream")

	code, out, errW := runStatus(t, l)
	if code != 0 {
		t.Fatalf("no upstream is a state, not an error: exit %d: %s", code, errW)
	}
	want := "public: clean, in sync with origin/main\nprivate: clean, no upstream\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}

	_, out, _ = runStatus(t, l, "--json")
	rows, _ := jsonRows(t, out)
	if rows[1]["upstream"] != nil || rows[1]["ahead"] != nil || rows[1]["behind"] != nil {
		t.Errorf("no-upstream row must carry nulls: %v", rows[1])
	}
	for _, k := range []string{"upstream", "ahead", "behind"} {
		if _, ok := rows[1][k]; !ok {
			t.Errorf("no-upstream row must still have key %s: %v", k, rows[1])
		}
	}
}

func TestStatus_ConflictExits2(t *testing.T) {
	l, _ := seedStatus(t)
	st.TargetFile{Path: ".examplerc", Content: "mine"}.Apply(t, l)

	code, out, errW := runStatus(t, l)
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, errW)
	}
	if errW != "" {
		t.Errorf("status reports conflicts on stdout, not as fatal errors; stderr = %q", errW)
	}
	want := "public: clean, in sync with origin/main\n" +
		"private: clean, in sync with origin/main\n" +
		"conflict .examplerc: .examplerc exists and is not a dotfiles symlink (dfm public .examplerc to adopt it, or remove it and rerun)\n" +
		"install is blocked by 1 conflict\n"
	if out != want {
		t.Errorf("stdout = %q\nwant     %q", out, want)
	}
	if got, _ := os.ReadFile(filepath.Join(l.Target, ".examplerc")); string(got) != "mine" {
		t.Errorf("status touched the conflicting file: %q", got)
	}
}

func TestStatus_BrokenLink(t *testing.T) {
	l, _ := seedStatus(t)
	st.PkgFile{Pkg: "public", Path: ".gone"}.Apply(t, l)
	mustInstall(t, l)
	st.PkgRemove{Pkg: "public", Path: ".gone"}.Apply(t, l)

	code, out, errW := runStatus(t, l)
	if code != 0 {
		t.Fatalf("a broken link is not a conflict: exit %d: %s", code, errW)
	}
	want := "public: clean, in sync with origin/main\n" +
		"private: clean, in sync with origin/main\n" +
		"broken link .gone -> dotfiles/public/.gone (public)\n" +
		"install would apply 1 action\n"
	if out != want {
		t.Errorf("stdout = %q\nwant     %q", out, want)
	}
	if _, err := os.Lstat(filepath.Join(l.Target, ".gone")); err != nil {
		t.Errorf("status removed the broken link: %v", err)
	}
}

func TestStatus_JSONShape(t *testing.T) {
	l, _ := seedStatus(t)
	gittest.Commit(t, filepath.Join(l.Root, "public"), ".gone", "", "tracked")
	gittest.Push(t, filepath.Join(l.Root, "public"))
	mustInstall(t, l)
	st.PkgRemove{Pkg: "public", Path: ".gone"}.Apply(t, l)     // broken link, public dirty (deleted)
	st.PkgFile{Pkg: "private", Path: ".examplerc"}.Apply(t, l) // conflicts with public, private dirty
	st.TargetFile{Path: ".foreign", Content: "x"}.Apply(t, l)
	st.PkgFile{Pkg: "private", Path: ".foreign"}.Apply(t, l) // conflicts with target file

	code, out, errW := runStatus(t, l, "--json")
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, errW)
	}
	if errW != "" {
		t.Errorf("--json must write nothing to stderr, got %q", errW)
	}
	rows, meta := jsonRows(t, out)

	want := []map[string]any{
		{"kind": "package", "package": "public", "dirty": true, "changes": 1.0, "upstream": "origin/main", "ahead": 0.0, "behind": 0.0},
		{"kind": "package", "package": "private", "dirty": true, "changes": 2.0, "upstream": "origin/main", "ahead": 0.0, "behind": 0.0},
		{"kind": "conflict", "package": "private", "path": ".examplerc",
			"detail": ".examplerc is already linked to dotfiles/public/.examplerc, which belongs to another package",
			"hint":   "remove it and rerun"},
		{"kind": "conflict", "package": "private", "path": ".foreign",
			"detail": ".foreign exists and is not a dotfiles symlink",
			"hint":   "dfm private .foreign to adopt it, or remove it and rerun"},
		{"kind": "broken_link", "package": "public", "path": ".gone", "target": "dotfiles/public/.gone"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v\nwant %v", rows, want)
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
	wantMeta := map[string]any{"has_more": false, "dirty": 2.0, "conflicts": 2.0, "broken_links": 1.0, "pending": 1.0}
	if len(meta) != len(wantMeta) {
		t.Errorf("_meta = %v, want %v", meta, wantMeta)
	}
	for k, v := range wantMeta {
		if meta[k] != v {
			t.Errorf("_meta.%s = %v, want %v", k, meta[k], v)
		}
	}
}

func TestStatus_JSONMetaOmitsZeroCounts(t *testing.T) {
	l, _ := seedStatus(t)
	mustInstall(t, l)
	_, out, _ := runStatus(t, l, "--json")
	_, meta := jsonRows(t, out)
	if len(meta) != 1 || meta["has_more"] != false {
		t.Errorf("clean _meta should be only has_more, got %v", meta)
	}
}

func TestStatus_TreeUnchanged(t *testing.T) {
	l, _ := seedStatus(t)
	st.PkgFile{Pkg: "public", Path: ".gone"}.Apply(t, l)
	mustInstall(t, l)
	// Every kind of finding at once: dirty repo, pending link, broken link,
	// conflict. Status must report all of it and write nothing.
	st.PkgRemove{Pkg: "public", Path: ".gone"}.Apply(t, l)
	st.PkgFile{Pkg: "private", Path: ".pending"}.Apply(t, l)
	st.TargetRemove{Path: ".privaterc"}.Apply(t, l)
	st.TargetFile{Path: ".privaterc", Content: "mine"}.Apply(t, l)

	before := hashTree(t, l.Target)
	for _, extra := range [][]string{nil, {"--json"}, {"--verbose"}, {"--dry-run"}} {
		code, _, errW := runStatus(t, l, extra...)
		if code != 2 {
			t.Fatalf("%v: exit %d, want 2: %s", extra, code, errW)
		}
	}
	if after := hashTree(t, l.Target); after != before {
		t.Error("status changed something under the target or the packages")
	}
}

func TestStatus_PackageMissing(t *testing.T) {
	l, _ := seedStatus(t)
	if err := os.RemoveAll(filepath.Join(l.Root, "private")); err != nil {
		t.Fatal(err)
	}
	code, out, errW := runStatus(t, l)
	if code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, errW)
	}
	if out != "" {
		t.Errorf("stdout should be empty, got %q", out)
	}
	if m := oneJSONObject(t, errW); m["error"] != "package_missing" {
		t.Errorf("error = %v, want package_missing", m["error"])
	}
}

func TestStatus_NotARepoExits4(t *testing.T) {
	l, _ := seedStatus(t)
	if err := os.RemoveAll(filepath.Join(l.Root, "private", ".git")); err != nil {
		t.Fatal(err)
	}
	code, out, errW := runStatus(t, l)
	if code != 4 {
		t.Fatalf("exit %d, want 4: %s", code, errW)
	}
	if out != "" {
		t.Errorf("stdout should be empty on a fatal error, got %q", out)
	}
	m := oneJSONObject(t, errW)
	if m["error"] != "git_failed" {
		t.Errorf("error = %v, want git_failed", m["error"])
	}
	if p, _ := m["path"].(string); !strings.HasSuffix(p, filepath.Join("private")) {
		t.Errorf("path = %q should name the private package dir", p)
	}
}
