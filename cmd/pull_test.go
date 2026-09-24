package cmd_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/tammersaleh/dotfiles-manager/internal/gitx/gittest"
	st "github.com/tammersaleh/dotfiles-manager/internal/stow/stowtest"
)

// Layer 3 (SPEC.md "Testing"): a bare repo per package plays the remote, a
// second clone plays another machine. Every test here isolates HOME and the
// gitconfig through seedStatus -> gittest.Isolate, so none can t.Parallel.

// Extra flags follow the command so --no-hooks parses; Kong accepts the
// global flags there too.
func runPull(t *testing.T, l st.Layout, extra ...string) (int, string, string) {
	t.Helper()
	args := []string{"--root", l.Root, "--target", l.Target, "pull"}
	return run(t, append(args, extra...)...)
}

// wantPullError asserts a failed pull: exit code, empty stdout, and a
// stderr whose last line is the fatal JSON object with the given code.
// Progress lines may precede it in human mode; every earlier line must be
// plain text, not a JSON object.
func wantPullError(t *testing.T, code, wantCode int, out, errW, wantErr string) map[string]any {
	t.Helper()
	if code != wantCode {
		t.Fatalf("exit %d, want %d\nstdout %s\nstderr %s", code, wantCode, out, errW)
	}
	if out != "" {
		t.Errorf("stdout should be empty on error, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(errW, "\n"), "\n")
	for _, line := range lines[:len(lines)-1] {
		if strings.HasPrefix(line, "{") {
			t.Errorf("only the last stderr line may be JSON: %q", line)
		}
	}
	m := oneJSONObject(t, lines[len(lines)-1]+"\n")
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

// pushElsewhere commits content to path on another machine and pushes, so
// the package's remote is one commit ahead of the local clone.
func pushElsewhere(t *testing.T, remote, path, content string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	gittest.Clone(t, remote, other)
	gittest.Commit(t, other, path, content, "elsewhere: "+path)
	gittest.Push(t, other)
}

// pkgHead is the full SHA of HEAD in a package clone.
func pkgHead(t *testing.T, l st.Layout, pkg string) string {
	t.Helper()
	return gittest.Git(t, filepath.Join(l.Root, pkg), "rev-parse", "HEAD")
}

// remoteHead is the full SHA of main on a package's remote.
func remoteHead(t *testing.T, remote string) string {
	t.Helper()
	return gittest.Git(t, remote, "rev-parse", "main")
}

// commitHook writes <pkg>/post-pull.sh with body and mode, adds a
// .stow-local-ignore that keeps .git and the hook out of the target (as the
// real packages do), commits, and pushes so the clone stays clean and in
// sync. body is a POSIX sh script without the shebang.
func commitHook(t *testing.T, l st.Layout, pkg, body string, mode os.FileMode) {
	t.Helper()
	dir := filepath.Join(l.Root, pkg)
	ignore := filepath.Join(dir, ".stow-local-ignore")
	if err := os.WriteFile(ignore, []byte("\\.git\npost-pull\\.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(dir, "post-pull.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n"+body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(hook, mode); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, dir, "add", "--", ".stow-local-ignore", "post-pull.sh")
	gittest.Git(t, dir, "commit", "--quiet", "-m", "add hook")
	gittest.Push(t, dir)
}

// logHook is a hook body that appends "<tag> <cwd>" to log, echoes tag to
// stdout, and exits with code.
func logHook(tag, log string, code int) string {
	return "printf '%s %s\\n' " + tag + " \"$PWD\" >> " + log + "\necho hello from " + tag + "\nexit " + strconv.Itoa(code) + "\n"
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func TestPull_RemoteAhead(t *testing.T) {
	l, remotes := seedStatus(t)
	mustInstall(t, l)
	pushElsewhere(t, remotes["public"], ".newpub", "1\n")
	pushElsewhere(t, remotes["private"], ".config/example/rc", "2\n")
	pubBefore, privBefore := pkgHead(t, l, "public"), pkgHead(t, l, "private")

	code, out, errW := runPull(t, l)
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errW)
	}
	if out != "" {
		t.Errorf("git status --short after a clean rebase is empty, so stdout should be too: %q", out)
	}
	for _, pkg := range []string{"public", "private"} {
		if got, want := pkgHead(t, l, pkg), remoteHead(t, remotes[pkg]); got != want {
			t.Errorf("%s HEAD = %s, want remote main %s", pkg, got, want)
		}
	}
	for _, want := range []string{
		"pulling public...", "public: " + pubBefore[:7] + ".." + pkgHead(t, l, "public")[:7],
		"pulling private...", "private: " + privBefore[:7] + ".." + pkgHead(t, l, "private")[:7],
		"LINK .newpub -> dotfiles/public/.newpub",
		"LINK .config -> dotfiles/private/.config",
		"created 2, removed 0",
	} {
		if !strings.Contains(errW, want) {
			t.Errorf("stderr missing %q:\n%s", want, errW)
		}
	}
	want := []st.Entry{
		{Path: ".config", Kind: "link", Link: "dotfiles/private/.config"},
		{Path: ".examplerc", Kind: "link", Link: "dotfiles/public/.examplerc"},
		{Path: ".newpub", Kind: "link", Link: "dotfiles/public/.newpub"},
		{Path: ".privaterc", Kind: "link", Link: "dotfiles/private/.privaterc"},
	}
	if d := st.Diff(want, st.Snapshot(t, l)); d != "" {
		t.Errorf("tree (-want +got):\n%s", d)
	}

	// Idempotent: a second pull moves nothing.
	code, _, errW = runPull(t, l)
	if code != 0 || !strings.Contains(errW, "public: already up to date") || !strings.Contains(errW, "nothing to do") {
		t.Errorf("second pull: exit %d\n%s", code, errW)
	}
}

func TestPull_DirtyTree(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(t *testing.T, l st.Layout)
		wantNamed []string
		wantClean []string
	}{
		{"private modified", func(t *testing.T, l st.Layout) {
			if err := os.WriteFile(filepath.Join(l.Root, "private", ".privaterc"), []byte("edited\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, []string{"private"}, []string{"public"}},
		{"both dirty", func(t *testing.T, l st.Layout) {
			for _, pkg := range []string{"public", "private"} {
				st.PkgFile{Pkg: pkg, Path: ".scratch"}.Apply(t, l)
			}
		}, []string{"public", "private"}, nil},
		{"untracked only", func(t *testing.T, l st.Layout) {
			st.PkgFile{Pkg: "public", Path: ".untracked"}.Apply(t, l)
		}, []string{"public"}, []string{"private"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, remotes := seedStatus(t)
			mustInstall(t, l)
			for _, pkg := range []string{"public", "private"} {
				pushElsewhere(t, remotes[pkg], ".from-"+pkg, "x\n")
			}
			tt.mutate(t, l)
			heads := map[string]string{"public": pkgHead(t, l, "public"), "private": pkgHead(t, l, "private")}
			before := hashTree(t, l.Target)

			code, out, errW := runPull(t, l)
			m := wantError(t, code, 1, out, errW, "dirty_tree")
			detail, _ := m["detail"].(string)
			for _, pkg := range tt.wantNamed {
				if !strings.Contains(detail, pkg) {
					t.Errorf("detail %q must name %s", detail, pkg)
				}
			}
			for _, pkg := range tt.wantClean {
				if strings.Contains(detail, pkg) {
					t.Errorf("detail %q must not name the clean package %s", detail, pkg)
				}
			}
			for pkg, head := range heads {
				if pkgHead(t, l, pkg) != head {
					t.Errorf("%s HEAD moved on a refused pull", pkg)
				}
				if _, err := os.Stat(filepath.Join(l.Root, pkg, ".git", "FETCH_HEAD")); err == nil {
					t.Errorf("%s was fetched despite the dirty tree", pkg)
				}
			}
			if hashTree(t, l.Target) != before {
				t.Error("a refused pull changed something under the target or the packages")
			}
		})
	}
}

func TestPull_HooksRunInOrderWithCwd(t *testing.T) {
	l, _ := seedStatus(t)
	log := filepath.Join(t.TempDir(), "hooks.log")
	commitHook(t, l, "public", logHook("public", log, 0), 0o755)
	commitHook(t, l, "private", logHook("private", log, 0), 0o755)

	code, out, errW := runPull(t, l)
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errW)
	}
	want := []string{
		"public " + filepath.Join(l.Root, "public"),
		"private " + filepath.Join(l.Root, "private"),
	}
	got := readLines(t, log)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("hook log = %q, want %q (order and $PWD)", got, want)
	}
	if out != "hello from public\nhello from private\n" {
		t.Errorf("hook stdout must pass through to dfm's stdout in order, got %q", out)
	}
	for _, want := range []string{"running public/post-pull.sh", "running private/post-pull.sh"} {
		if !strings.Contains(errW, want) {
			t.Errorf("stderr missing %q:\n%s", want, errW)
		}
	}
	if i, j := strings.Index(errW, "running public/"), strings.Index(errW, "running private/"); i > j {
		t.Errorf("hook progress out of order:\n%s", errW)
	}
	// The hook itself is never linked into the target.
	if _, err := os.Lstat(filepath.Join(l.Target, "post-pull.sh")); err == nil {
		t.Error("post-pull.sh was linked into the target")
	}
}

func TestPull_HookFails(t *testing.T) {
	l, _ := seedStatus(t)
	log := filepath.Join(t.TempDir(), "hooks.log")
	commitHook(t, l, "public", logHook("public", log, 3), 0o755)
	commitHook(t, l, "private", logHook("private", log, 0), 0o755)

	code, out, errW := runPull(t, l)
	if code != 1 {
		t.Fatalf("exit %d, want 1\nstdout %s\nstderr %s", code, out, errW)
	}
	// The hook's own output reaches stdout before the fatal error.
	if out != "hello from public\n" {
		t.Errorf("stdout = %q", out)
	}
	lines := strings.Split(strings.TrimRight(errW, "\n"), "\n")
	m := oneJSONObject(t, lines[len(lines)-1]+"\n")
	if m["error"] != "hook_failed" {
		t.Errorf("error = %v, want hook_failed", m["error"])
	}
	if !strings.Contains(errW, "running public/post-pull.sh") || strings.Contains(errW, "running private/") {
		t.Errorf("progress must show public running and private never announced:\n%s", errW)
	}
	detail, _ := m["detail"].(string)
	if !strings.Contains(detail, "public") || !strings.Contains(detail, "3") {
		t.Errorf("detail %q must carry the package and the exit code", detail)
	}
	if p, _ := m["path"].(string); p != filepath.Join(l.Root, "public", "post-pull.sh") {
		t.Errorf("path = %q", p)
	}
	if h, _ := m["hint"].(string); h == "" {
		t.Error("hint missing")
	}
	if got := readLines(t, log); len(got) != 1 || !strings.HasPrefix(got[0], "public ") {
		t.Errorf("private hook must not run after public fails; log = %q", got)
	}
}

func TestPull_HookNotExecutable(t *testing.T) {
	l, _ := seedStatus(t)
	log := filepath.Join(t.TempDir(), "hooks.log")
	commitHook(t, l, "public", logHook("public", log, 0), 0o644)
	commitHook(t, l, "private", logHook("private", log, 0), 0o755)

	code, out, errW := runPull(t, l)
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errW)
	}
	if got := readLines(t, log); len(got) != 1 || !strings.HasPrefix(got[0], "private ") {
		t.Errorf("only the executable hook runs; log = %q", got)
	}
	if strings.Contains(errW, "running public/") {
		t.Errorf("skipped hook announced as running:\n%s", errW)
	}
	// The skip is visible only under --verbose.
	code, _, errW = runPull(t, l, "--verbose")
	if code != 0 || !strings.Contains(errW, "skipping public/post-pull.sh: not executable") {
		t.Errorf("verbose skip line missing: exit %d\n%s", code, errW)
	}
}

func TestPull_NoHooks(t *testing.T) {
	l, _ := seedStatus(t)
	log := filepath.Join(t.TempDir(), "hooks.log")
	commitHook(t, l, "public", logHook("public", log, 0), 0o755)
	commitHook(t, l, "private", logHook("private", log, 0), 0o755)

	code, out, errW := runPull(t, l, "--no-hooks")
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errW)
	}
	if got := readLines(t, log); got != nil {
		t.Errorf("--no-hooks ran a hook: %q", got)
	}
	if out != "" || strings.Contains(errW, "running ") {
		t.Errorf("--no-hooks output: stdout %q stderr %q", out, errW)
	}
}

func TestPull_BadRemoteExits4(t *testing.T) {
	tests := []struct {
		name   string
		broken string
		moved  []string // packages whose HEAD must have moved
		stayed []string // packages whose HEAD must not have moved
	}{
		{"public unreachable", "public", nil, []string{"public", "private"}},
		{"private unreachable", "private", []string{"public"}, []string{"private"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, remotes := seedStatus(t)
			mustInstall(t, l)
			for _, pkg := range []string{"public", "private"} {
				pushElsewhere(t, remotes[pkg], ".from-"+pkg, "x\n")
			}
			heads := map[string]string{"public": pkgHead(t, l, "public"), "private": pkgHead(t, l, "private")}
			gittest.Git(t, filepath.Join(l.Root, tt.broken), "remote", "set-url", "origin", filepath.Join(t.TempDir(), "nope.git"))

			code, out, errW := runPull(t, l)
			m := wantPullError(t, code, 4, out, errW, "git_failed")
			if d, _ := m["detail"].(string); !strings.Contains(d, tt.broken+": git fetch") {
				t.Errorf("detail %q must name the package and the subcommand", d)
			}
			for _, pkg := range tt.moved {
				if pkgHead(t, l, pkg) == heads[pkg] {
					t.Errorf("%s should have been pulled before the failure", pkg)
				}
			}
			for _, pkg := range tt.stayed {
				if pkgHead(t, l, pkg) != heads[pkg] {
					t.Errorf("%s HEAD moved", pkg)
				}
			}
			// Nothing after the failed package ran: no new links.
			if _, err := os.Lstat(filepath.Join(l.Target, ".from-public")); err == nil {
				t.Error("install ran after a git failure")
			}
		})
	}
}

func TestPull_RebaseConflictExits4(t *testing.T) {
	l, remotes := seedStatus(t)
	mustInstall(t, l)
	gittest.Commit(t, filepath.Join(l.Root, "public"), ".examplerc", "mine\n", "local edit")
	pushElsewhere(t, remotes["public"], ".examplerc", "theirs\n")

	code, out, errW := runPull(t, l)
	m := wantPullError(t, code, 4, out, errW, "git_failed")
	if d, _ := m["detail"].(string); !strings.Contains(d, "public: git rebase") {
		t.Errorf("detail = %q", d)
	}
	h, _ := m["hint"].(string)
	if !strings.Contains(h, "git rebase --abort") || !strings.Contains(h, filepath.Join(l.Root, "public")) {
		t.Errorf("hint = %q, want the abort command in the package dir", h)
	}
	// The hint works and leaves the local commit in place.
	gittest.Git(t, filepath.Join(l.Root, "public"), "rebase", "--abort")
	if b, _ := os.ReadFile(filepath.Join(l.Root, "public", ".examplerc")); string(b) != "mine\n" {
		t.Errorf("after abort .examplerc = %q", b)
	}
}

func TestPull_ConflictExits2AfterUpdate(t *testing.T) {
	l, remotes := seedStatus(t)
	mustInstall(t, l)
	log := filepath.Join(t.TempDir(), "hooks.log")
	commitHook(t, l, "public", logHook("public", log, 0), 0o755)
	pushElsewhere(t, remotes["public"], ".clash", "theirs\n")
	st.TargetFile{Path: ".clash", Content: "mine"}.Apply(t, l)

	code, out, errW := runPull(t, l)
	if code != 2 {
		t.Fatalf("exit %d, want 2\nstdout %s\nstderr %s", code, out, errW)
	}
	var jsonLines []string
	for _, line := range strings.Split(errW, "\n") {
		if strings.HasPrefix(line, "{") {
			jsonLines = append(jsonLines, line)
		}
	}
	if codes := stderrErrors(t, strings.Join(jsonLines, "\n")+"\n"); len(codes) != 1 || codes[0] != "conflict" {
		t.Errorf("stderr error codes = %v", codes)
	}
	if !strings.Contains(errW, "rerun dfm install after fixing") {
		t.Errorf("conflict hint must say the repos are already updated:\n%s", errW)
	}
	if pkgHead(t, l, "public") != remoteHead(t, remotes["public"]) {
		t.Error("public should be rebased before install detects the conflict")
	}
	if got, _ := os.ReadFile(filepath.Join(l.Target, ".clash")); string(got) != "mine" {
		t.Errorf("conflicting file touched: %q", got)
	}
	if readLines(t, log) != nil {
		t.Error("hooks ran after a conflict")
	}
}

func TestPull_DryRun(t *testing.T) {
	l, remotes := seedStatus(t)
	log := filepath.Join(t.TempDir(), "hooks.log")
	commitHook(t, l, "public", logHook("public", log, 0), 0o755)
	commitHook(t, l, "private", logHook("private", log, 0), 0o644)
	mustInstall(t, l)
	pushElsewhere(t, remotes["public"], ".newpub", "1\n")
	before := hashTree(t, l.Target)

	code, out, errW := runPull(t, l, "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errW)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if hashTree(t, l.Target) != before {
		t.Error("--dry-run changed the target or the packages (HEAD, FETCH_HEAD, or the tree)")
	}
	if pkgHead(t, l, "public") == remoteHead(t, remotes["public"]) {
		t.Error("--dry-run must not fetch: the local HEAD caught up with the remote")
	}
	if readLines(t, log) != nil {
		t.Error("--dry-run ran a hook")
	}
	for _, want := range []string{
		"dry run: would fetch public and rebase onto FETCH_HEAD",
		"dry run: would fetch private and rebase onto FETCH_HEAD",
		"nothing to do",
		"dry run: would run public/post-pull.sh",
	} {
		if !strings.Contains(errW, want) {
			t.Errorf("stderr missing %q:\n%s", want, errW)
		}
	}
	if strings.Contains(errW, "would run private/") {
		t.Errorf("not-executable hook reported as runnable:\n%s", errW)
	}

	// A dirty tree is reported even under --dry-run: the pull could not run.
	st.PkgFile{Pkg: "private", Path: ".scratch"}.Apply(t, l)
	code, out, errW = runPull(t, l, "--dry-run")
	wantError(t, code, 1, out, errW, "dirty_tree")
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestPull_JSONShape(t *testing.T) {
	l, remotes := seedStatus(t)
	commitHook(t, l, "public", "echo hello from public\n", 0o755)
	commitHook(t, l, "private", "exit 0\n", 0o644)
	mustInstall(t, l)
	pushElsewhere(t, remotes["public"], ".newpub", "1\n")
	pubBefore, privBefore := pkgHead(t, l, "public"), pkgHead(t, l, "private")

	code, out, errW := runPull(t, l, "--json")
	if code != 0 {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errW)
	}
	if errW != "hello from public\n" {
		t.Errorf("under --json the hook's stdout goes to stderr and nothing else does; stderr = %q", errW)
	}
	rows, meta := jsonRows(t, out)

	want := []map[string]any{
		{"action": "pull", "package": "public", "before": pubBefore, "after": remoteHead(t, remotes["public"])},
		{"action": "pull", "package": "private", "before": privBefore, "after": privBefore},
		{"action": "link", "package": "public", "path": ".newpub", "target": "dotfiles/public/.newpub"},
		{"action": "hook", "package": "public", "exit": 0.0},
		{"action": "skip", "package": "private", "reason": "not_executable"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v\nwant %v", rows, want)
	}
	for i := range want {
		for k, v := range want[i] {
			if rows[i][k] != v {
				t.Errorf("row %d %s = %v, want %v", i, k, rows[i][k], v)
			}
		}
		for k := range rows[i] {
			if _, ok := want[i][k]; !ok {
				t.Errorf("row %d has unexpected key %s: %v", i, k, rows[i])
			}
		}
	}
	for _, i := range []int{0, 1} {
		for _, k := range []string{"before", "after"} {
			if s, _ := rows[i][k].(string); !shaRE.MatchString(s) {
				t.Errorf("row %d %s = %v, want a full SHA", i, k, rows[i][k])
			}
		}
	}
	wantMeta := map[string]any{"has_more": false, "pulled": 2.0, "hooks": 1.0, "created": 1.0}
	if len(meta) != len(wantMeta) {
		t.Errorf("_meta = %v, want %v", meta, wantMeta)
	}
	for k, v := range wantMeta {
		if meta[k] != v {
			t.Errorf("_meta.%s = %v, want %v", k, meta[k], v)
		}
	}
}

func TestPull_JSONNoHooksAndDryRun(t *testing.T) {
	l, _ := seedStatus(t)
	commitHook(t, l, "public", "exit 0\n", 0o755)
	mustInstall(t, l)

	_, out, errW := runPull(t, l, "--json", "--no-hooks")
	if errW != "" {
		t.Errorf("stderr = %q", errW)
	}
	rows, meta := jsonRows(t, out)
	if len(rows) != 4 || rows[2]["reason"] != "no_hooks" || rows[3]["reason"] != "no_hooks" {
		t.Errorf("rows = %v", rows)
	}
	if meta["hooks"] != nil || meta["pulled"] != 2.0 {
		t.Errorf("_meta = %v", meta)
	}

	_, out, _ = runPull(t, l, "--json", "--dry-run")
	rows, meta = jsonRows(t, out)
	// pull public, pull private, hook public (would run), skip private (absent)
	if len(rows) != 4 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0]["after"] != nil || rows[2]["exit"] != nil || rows[3]["reason"] != "absent" {
		t.Errorf("dry-run rows must carry null after/exit: %v", rows)
	}
	for _, k := range []string{"after"} {
		if _, ok := rows[0][k]; !ok {
			t.Errorf("dry-run pull row must still have key %s", k)
		}
	}
	if _, ok := rows[2]["exit"]; !ok {
		t.Error("dry-run hook row must still have key exit")
	}
	if meta["pulled"] != 2.0 || meta["hooks"] != 1.0 {
		t.Errorf("dry-run _meta = %v", meta)
	}
}

func TestPull_PackageMissing(t *testing.T) {
	l, _ := seedStatus(t)
	if err := os.RemoveAll(filepath.Join(l.Root, "private")); err != nil {
		t.Fatal(err)
	}
	code, out, errW := runPull(t, l)
	wantError(t, code, 1, out, errW, "package_missing")
}

func TestPull_NotARepoExits4(t *testing.T) {
	l, _ := seedStatus(t)
	if err := os.RemoveAll(filepath.Join(l.Root, "private", ".git")); err != nil {
		t.Fatal(err)
	}
	code, out, errW := runPull(t, l)
	wantError(t, code, 4, out, errW, "git_failed")
}
