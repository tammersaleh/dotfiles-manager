package gitx_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tammersaleh/dotfiles-manager/internal/gitx"
	"github.com/tammersaleh/dotfiles-manager/internal/gitx/gittest"
	"github.com/tammersaleh/dotfiles-manager/internal/output"
)

// seed returns a cloned, pushed, in-sync repo and its remote.
func seed(t *testing.T) (dir, remote string) {
	t.Helper()
	gittest.Isolate(t)
	tmp := t.TempDir()
	remote = filepath.Join(tmp, "remote.git")
	gittest.InitRemote(t, remote)
	dir = filepath.Join(tmp, "clone")
	gittest.Clone(t, remote, dir)
	gittest.Commit(t, dir, ".examplerc", "one\n", "seed")
	gittest.Push(t, dir)
	return dir, remote
}

func TestStatus(t *testing.T) {
	dir, _ := seed(t)

	st, err := gitx.Status(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Dirty || len(st.Lines) != 0 {
		t.Errorf("clean clone reported %+v", st)
	}

	if err := os.WriteFile(filepath.Join(dir, ".examplerc"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = gitx.Status(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Dirty || len(st.Lines) != 2 {
		t.Errorf("modified + untracked: got %+v, want dirty with 2 lines", st)
	}
	want := []string{" M .examplerc", "?? untracked"}
	for i, w := range want {
		if i >= len(st.Lines) || st.Lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, st.Lines, w)
		}
	}
}

func TestStatus_NotARepo(t *testing.T) {
	gittest.Isolate(t)
	_, err := gitx.Status(t.TempDir())
	var oErr *output.Error
	if !errors.As(err, &oErr) {
		t.Fatalf("want *output.Error, got %v", err)
	}
	if oErr.Err != "git_failed" || oErr.ExitCode() != output.ExitGit {
		t.Errorf("got %+v, want git_failed exit 4", oErr)
	}
	if oErr.Path == "" || oErr.Detail == "" {
		t.Errorf("error must carry path and detail: %+v", oErr)
	}
}

func TestStatus_MissingDir(t *testing.T) {
	gittest.Isolate(t)
	_, err := gitx.Status(filepath.Join(t.TempDir(), "nope"))
	var oErr *output.Error
	if !errors.As(err, &oErr) || oErr.ExitCode() != output.ExitGit {
		t.Fatalf("want git_failed exit 4, got %v", err)
	}
}

func TestAheadBehind(t *testing.T) {
	dir, remote := seed(t)

	sync, err := gitx.AheadBehind(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sync.Upstream != "origin/main" || sync.Ahead != 0 || sync.Behind != 0 {
		t.Errorf("in sync: got %+v", sync)
	}

	// Ahead: a local commit not pushed.
	gittest.Commit(t, dir, ".examplerc", "two\n", "local")
	// Behind: another machine pushes two commits, and we fetch.
	other := filepath.Join(t.TempDir(), "other")
	gittest.Clone(t, remote, other)
	gittest.Commit(t, other, ".other", "a\n", "remote 1")
	gittest.Commit(t, other, ".other", "b\n", "remote 2")
	gittest.Push(t, other)
	gittest.Git(t, dir, "fetch", "--quiet")

	sync, err = gitx.AheadBehind(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sync.Upstream != "origin/main" || sync.Ahead != 1 || sync.Behind != 2 {
		t.Errorf("diverged: got %+v, want ahead 1 behind 2", sync)
	}
}

func TestAheadBehind_NoUpstream(t *testing.T) {
	dir, _ := seed(t)
	gittest.Git(t, dir, "branch", "--unset-upstream")

	sync, err := gitx.AheadBehind(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sync.Upstream != "" {
		t.Errorf("want no upstream, got %+v", sync)
	}
}

func TestAheadBehind_DetachedHead(t *testing.T) {
	dir, _ := seed(t)
	gittest.Git(t, dir, "checkout", "--quiet", "--detach")

	sync, err := gitx.AheadBehind(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sync.Upstream != "" {
		t.Errorf("detached HEAD should report no upstream, got %+v", sync)
	}
}

func TestAheadBehind_NotARepo(t *testing.T) {
	gittest.Isolate(t)
	_, err := gitx.AheadBehind(t.TempDir())
	var oErr *output.Error
	if !errors.As(err, &oErr) || oErr.Err != "git_failed" || oErr.ExitCode() != output.ExitGit {
		t.Fatalf("want git_failed exit 4, got %v", err)
	}
}

// TestStatus_HonorsGlobalExcludes pins the parity decision: a file hidden
// by the user's global excludes file is not dirty, exactly as `git status`
// by hand would report.
func TestStatus_HonorsGlobalExcludes(t *testing.T) {
	dir, _ := seed(t)
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	excludes := filepath.Join(t.TempDir(), "excludes")
	if err := os.WriteFile(excludes, []byte(".DS_Store\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("[core]\n\texcludesFile = "+excludes+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := gitx.Status(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Dirty {
		t.Errorf("globally excluded file reported dirty: %+v", st)
	}
}
