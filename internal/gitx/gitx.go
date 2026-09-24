// Package gitx shells out to git for the package repositories. No go-git.
// The user's gitconfig is honored, as the bash script's plain `git status`
// did (a global core.excludesFile must hide the same files here). Every
// process runs with GIT_TERMINAL_PROMPT=0 so nothing ever prompts; queries
// additionally run with GIT_OPTIONAL_LOCKS=0 so they never touch the index,
// while fetch and rebase get a normal environment because they write. Tests
// isolate the config through the environment (gittest.Isolate). Failures
// are *output.Error with code git_failed, exit 4.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tammersaleh/dotfiles-manager/internal/output"
)

// TreeStatus is the working-tree state of one repository.
type TreeStatus struct {
	Dirty bool
	Lines []string // `git status --porcelain --untracked-files` lines, in git's order
}

// Sync is a repository's position relative to its upstream. Upstream is
// empty when the checked-out branch has none (or HEAD is detached); Ahead
// and Behind are then zero and meaningless.
type Sync struct {
	Upstream string
	Ahead    int
	Behind   int
}

// Status runs `git status --porcelain --untracked-files` in dir. An
// untracked file counts as dirty.
func Status(dir string) (TreeStatus, error) {
	out, err := query(dir, "status", "--porcelain", "--untracked-files")
	if err != nil {
		return TreeStatus{}, err
	}
	var st TreeStatus
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		st.Lines = append(st.Lines, line)
	}
	st.Dirty = len(st.Lines) > 0
	return st, nil
}

// noUpstreamMarkers are the stderr phrases git uses when @{upstream} does
// not resolve for a reason that is a state, not a failure.
var noUpstreamMarkers = []string{
	"no upstream configured",
	"does not point to a branch",
	"not stored as a remote-tracking branch",
}

// AheadBehind resolves the current branch's upstream and counts commits on
// each side. No fetch: the numbers are against the local tracking ref.
func AheadBehind(dir string) (Sync, error) {
	upstream, err := query(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		var oErr *output.Error
		if errors.As(err, &oErr) {
			for _, m := range noUpstreamMarkers {
				if strings.Contains(oErr.Detail, m) {
					return Sync{}, nil
				}
			}
		}
		return Sync{}, err
	}
	upstream = strings.TrimSpace(upstream)

	out, err := query(dir, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	if err != nil {
		return Sync{}, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return Sync{}, gitError(dir, "rev-list", fmt.Sprintf("unexpected output %q", out))
	}
	behind, err1 := strconv.Atoi(fields[0])
	ahead, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return Sync{}, gitError(dir, "rev-list", fmt.Sprintf("unexpected output %q", out))
	}
	return Sync{Upstream: upstream, Ahead: ahead, Behind: behind}, nil
}

// ShortStatus returns the raw `git status --short --untracked-files`
// output, which pull echoes after each rebase.
func ShortStatus(dir string) (string, error) {
	return query(dir, "status", "--short", "--untracked-files")
}

// HeadSHA returns the full object name of HEAD.
func HeadSHA(dir string) (string, error) {
	out, err := query(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Fetch runs `git fetch --all --quiet` in dir.
func Fetch(dir string) error {
	_, err := run(dir, false, "fetch", "--all", "--quiet")
	return err
}

// Rebase runs `git rebase <onto> --quiet` in dir. When the rebase stops on
// a conflict the returned error's hint says how to abort it; the repository
// is left mid-rebase, exactly as git leaves it.
func Rebase(dir, onto string) error {
	_, err := run(dir, false, "rebase", onto, "--quiet")
	if err == nil {
		return nil
	}
	var oErr *output.Error
	if errors.As(err, &oErr) && inRebase(dir) {
		oErr.Hint = fmt.Sprintf("the rebase stopped on a conflict; run `git rebase --abort` in %s, resolve by hand, and rerun", dir)
	}
	return err
}

// inRebase reports whether dir has a rebase in progress.
func inRebase(dir string) bool {
	for _, state := range []string{"rebase-merge", "rebase-apply"} {
		out, err := query(dir, "rev-parse", "--git-path", state)
		if err != nil {
			continue
		}
		path := strings.TrimSpace(out)
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// query runs a read-only git command: GIT_OPTIONAL_LOCKS=0 keeps it from
// refreshing the index or taking locks.
func query(dir string, args ...string) (string, error) {
	return run(dir, true, args...)
}

// run executes git with args in dir and returns stdout. A non-zero exit or
// a failure to start git is a git_failed error carrying git's stderr.
func run(dir string, readOnly bool, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if readOnly {
		cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", gitError(dir, args[0], detail)
	}
	return stdout.String(), nil
}

func gitError(dir, subcommand, detail string) *output.Error {
	return &output.Error{
		Err:    "git_failed",
		Detail: fmt.Sprintf("git %s: %s", subcommand, detail),
		Hint:   "check that the package is a git clone and git is on PATH",
		Path:   dir,
		Code:   output.ExitGit,
	}
}
