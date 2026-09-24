// Package gittest is the layer 3 test harness (SPEC.md "Testing"): a bare
// repository in a temp dir plays the remote, clones under the dotfiles root
// play the packages, and a second clone plays another machine. Nothing
// touches the network or the real gitconfig.
package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Isolate points HOME and XDG_CONFIG_HOME at temp dirs for the rest of the
// test so neither the real gitconfig nor the real global excludes file can
// leak into git invocations made by the code under test, and gives those
// invocations a fixed author so a rebase that replays commits has an
// identity. Incompatible with t.Parallel.
func Isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GIT_AUTHOR_NAME", "alice")
	t.Setenv("GIT_AUTHOR_EMAIL", "alice@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "alice")
	t.Setenv("GIT_COMMITTER_EMAIL", "alice@example.com")
}

// Env is the environment for every git process the harness runs: hermetic
// config, a fixed author, no prompts.
func Env() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=alice",
		"GIT_AUTHOR_EMAIL=alice@example.com",
		"GIT_COMMITTER_NAME=alice",
		"GIT_COMMITTER_EMAIL=alice@example.com",
	)
}

// Git runs git in dir and returns trimmed stdout. Fails the test on error.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = Env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// InitRemote creates a bare repository at path whose default branch is main.
func InitRemote(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, path, "init", "--bare", "--quiet", "-b", "main")
}

// Clone clones remote into dest (which may be an existing empty dir).
func Clone(t *testing.T, remote, dest string) {
	t.Helper()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dest, "clone", "--quiet", remote, ".")
}

// Commit writes content to path (relative to dir), stages it, and commits.
func Commit(t *testing.T, dir, path, content, message string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "add", "--", path)
	Git(t, dir, "commit", "--quiet", "-m", message)
}

// Push pushes main to origin and sets it as the upstream.
func Push(t *testing.T, dir string) {
	t.Helper()
	Git(t, dir, "push", "--quiet", "-u", "origin", "main")
}

// Seed gives each package under root a bare remote (under remotes), clones
// it into <root>/<pkg>, commits one seed file per package, and pushes.
// Returns the remote path per package. Seed files: public/.examplerc and
// private/.privaterc.
func Seed(t *testing.T, root, remotes string) map[string]string {
	t.Helper()
	seeds := map[string]string{"public": ".examplerc", "private": ".privaterc"}
	out := map[string]string{}
	for _, pkg := range []string{"public", "private"} {
		remote := filepath.Join(remotes, pkg+".git")
		InitRemote(t, remote)
		dir := filepath.Join(root, pkg)
		Clone(t, remote, dir)
		Commit(t, dir, seeds[pkg], "# seed\n", "seed "+pkg)
		Push(t, dir)
		out[pkg] = remote
	}
	return out
}
