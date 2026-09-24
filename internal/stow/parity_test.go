package stow_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tammersaleh/dotfiles-manager/internal/stow"
	st "github.com/tammersaleh/dotfiles-manager/internal/stow/stowtest"
)

// parityFixtures are step sequences run against both stow 2.4.1 and dfm.
// They carry no expectations beyond "the two trees match": stow is the
// oracle. Hand-written expectations for the same shapes live in
// cmd/install_test.go (layer 2).
var parityFixtures = []st.Fixture{
	{Name: "single package folds", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgFile{Pkg: "public", Path: ".example/rc"},
		st.PkgFile{Pkg: "public", Path: ".example/sub/deep"},
		st.Run{},
	}},
	{Name: "two packages unfold one dir", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "public", Path: ".config/top.toml"},
		st.Run{},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		st.Run{StowDies: true},
	}},
	{Name: "unfold at two levels", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/shared/a"},
		st.Run{},
		st.PkgFile{Pkg: "private", Path: ".config/shared/b"},
		st.Run{StowDies: true},
	}},
	{Name: "deep unfold gives long relative links", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/a/b/c/d"},
		st.PkgFile{Pkg: "private", Path: ".config/a/b/c/e"},
		st.Run{},
	}},
	{Name: "second owner file deleted", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		st.Run{},
		st.PkgRemove{Pkg: "private", Path: ".config/beta"},
		st.Run{},
		st.Run{},
	}},
	{Name: "second owner dir deleted entirely", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		st.Run{},
		st.PkgRemove{Pkg: "private", Path: ".config"},
		st.Run{},
	}},
	{Name: "first owner dir deleted entirely", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		st.Run{},
		st.PkgRemove{Pkg: "public", Path: ".config"},
		st.Run{},
	}},
	{Name: "second owner files become ignored", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		st.Run{},
		st.PkgIgnore{Pkg: "private", Lines: []string{"beta"}},
		st.Run{},
	}},
	{Name: "symlink inside package linked as file", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/example/init.lua"},
		st.PkgSymlink{Pkg: "public", Path: ".example", Dest: ".config/example"},
		st.Run{},
	}},
	{Name: "symlink inside package pointing at a file", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgSymlink{Pkg: "public", Path: ".examplerc-alias", Dest: ".examplerc"},
		st.Run{},
	}},
	{Name: "absolute symlink inside package conflicts", Steps: []st.Step{
		st.PkgSymlink{Pkg: "public", Path: ".hosts", Dest: "/etc/hosts"},
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.Run{},
	}},
	{Name: "ignore list present replaces defaults", Steps: []st.Step{
		st.PkgIgnore{Pkg: "public", Lines: []string{`\.gitignore`, `README\.md`, `.*\.swp`, `\.claude/settings\.local\.json`, "# comment", ""}},
		st.PkgFile{Pkg: "public", Path: ".gitignore"},
		st.PkgFile{Pkg: "public", Path: "README.md"},
		st.PkgFile{Pkg: "public", Path: "LICENSE"}, // ignored by default only
		st.PkgFile{Pkg: "public", Path: ".example/README.md"},
		st.PkgFile{Pkg: "public", Path: ".example/notes.swp"},
		st.PkgFile{Pkg: "public", Path: ".claude/settings.local.json"},
		st.PkgFile{Pkg: "public", Path: ".claude/settings.json"},
		st.PkgFile{Pkg: "public", Path: ".claude/deeper/settings.local.json"},
		st.PkgFile{Pkg: "private", Path: ".claude/other.json"},
		st.Run{},
	}},
	{Name: "ignore list absent uses stow defaults", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".gitignore"},
		st.PkgFile{Pkg: "public", Path: ".gitmodules"},
		st.PkgFile{Pkg: "public", Path: "README.md"},
		st.PkgFile{Pkg: "public", Path: "LICENSE.txt"},
		st.PkgFile{Pkg: "public", Path: "COPYING"},
		st.PkgFile{Pkg: "public", Path: ".example/README.md"}, // README only ignored at top level
		st.PkgFile{Pkg: "public", Path: ".example/backup~"},
		st.PkgFile{Pkg: "public", Path: ".example/#autosave#"},
		st.PkgFile{Pkg: "public", Path: ".example/.#lock"},
		st.PkgFile{Pkg: "public", Path: ".example/file,v"},
		st.PkgFile{Pkg: "public", Path: ".example/CVS/Root"},
		st.PkgFile{Pkg: "public", Path: ".example/keep.swp"}, // not in defaults
		st.PkgFile{Pkg: "public", Path: ".example/rc"},
		st.Run{},
	}},
	{Name: "ignore list with inline comment and escaped hash", Steps: []st.Step{
		st.PkgIgnore{Pkg: "public", Lines: []string{`skip\.me   # trailing comment`, `\#hash`}},
		st.PkgFile{Pkg: "public", Path: "skip.me"},
		st.PkgFile{Pkg: "public", Path: "#hash"},
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.Run{},
	}},
	{Name: "conflict with regular file changes nothing", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgFile{Pkg: "public", Path: ".other"},
		st.TargetFile{Path: ".examplerc", Content: "mine"},
		st.Run{},
		st.TargetRemove{Path: ".examplerc"},
		st.Run{},
	}},
	{Name: "conflict with foreign symlink", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.TargetSymlink{Path: ".examplerc", Dest: "/etc/hosts"},
		st.Run{},
	}},
	{Name: "conflict with relative foreign symlink", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.TargetFile{Path: "real", Content: "x"},
		st.TargetSymlink{Path: ".examplerc", Dest: "real"},
		st.Run{},
	}},
	{Name: "conflict directory where file wanted", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.TargetDir{Path: ".examplerc"},
		st.Run{},
	}},
	{Name: "conflict file where directory wanted", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".example/rc"},
		st.TargetFile{Path: ".example", Content: "x"},
		st.Run{},
	}},
	{Name: "same path in both packages conflicts", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgFile{Pkg: "private", Path: ".examplerc"},
		st.Run{},
	}},
	{Name: "second package file under first package folded dir conflicts", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".example/rc"},
		st.Run{},
		st.PkgFile{Pkg: "private", Path: ".example/rc"},
		st.Run{StowDies: true},
	}},
	{Name: "pre-existing real directory in target is populated not folded", Steps: []st.Step{
		st.TargetDir{Path: ".config"},
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.Run{},
	}},
	{Name: "pre-existing real directory with foreign file", Steps: []st.Step{
		st.TargetFile{Path: ".config/theirs.toml", Content: "x"},
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.Run{},
		st.PkgRemove{Pkg: "public", Path: ".config"},
		st.Run{},
	}},
	{Name: "dangling link after package file deleted", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgFile{Pkg: "public", Path: ".other"},
		st.Run{},
		st.PkgRemove{Pkg: "public", Path: ".examplerc"},
		st.Run{},
	}},
	{Name: "dangling folded dir link after package dir deleted", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".example/rc"},
		st.PkgFile{Pkg: "public", Path: ".other"},
		st.Run{},
		st.PkgRemove{Pkg: "public", Path: ".example"},
		st.Run{},
	}},
	{Name: "dangling link inside unfolded dir", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "public", Path: ".config/gamma.toml"},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		st.Run{},
		st.PkgRemove{Pkg: "public", Path: ".config/gamma.toml"},
		st.Run{},
	}},
	{Name: "dangling link in subdir the package no longer has", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/one"},
		st.PkgFile{Pkg: "private", Path: ".config/alpha/two"},
		st.Run{},
		st.PkgRemove{Pkg: "private", Path: ".config/alpha"},
		st.PkgRemove{Pkg: "public", Path: ".config/alpha"},
		st.Run{},
	}},
	{Name: "file replaced by directory in package", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".example"},
		st.Run{},
		st.PkgRemove{Pkg: "public", Path: ".example"},
		st.PkgFile{Pkg: "public", Path: ".example/rc"},
		st.Run{},
	}},
	{Name: "directory replaced by file in package", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".example/rc"},
		st.Run{},
		st.PkgRemove{Pkg: "public", Path: ".example"},
		st.PkgFile{Pkg: "public", Path: ".example"},
		st.Run{},
	}},
	{Name: "moving a file between packages", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		st.Run{},
		st.PkgRemove{Pkg: "public", Path: ".config/alpha"},
		st.PkgFile{Pkg: "private", Path: ".config/alpha/config.toml"},
		st.Run{},
	}},
	{Name: "package with empty directory", Steps: []st.Step{
		st.PkgDir{Pkg: "public", Path: ".empty"},
		st.PkgDir{Pkg: "public", Path: ".config/empty"},
		st.PkgFile{Pkg: "private", Path: ".config/beta"},
		st.Run{},
	}},
	{Name: "executable bit and content are irrelevant", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: "bin/tool", Content: "#!/bin/sh\n", Exec: true},
		st.PkgFile{Pkg: "private", Path: "bin/secret-tool", Content: "#!/bin/sh\n", Exec: true},
		st.Run{},
	}},
	{Name: "dfm twice yields zero actions", Steps: []st.Step{
		st.PkgFile{Pkg: "public", Path: ".config/alpha/config.toml"},
		st.PkgFile{Pkg: "private", Path: ".config/beta/config.toml"},
		st.PkgFile{Pkg: "public", Path: ".examplerc"},
		st.PkgSymlink{Pkg: "public", Path: ".example", Dest: ".config/alpha"},
		st.Run{},
		st.Run{},
	}},
}

// allFixtures is parityFixtures plus the real-shape fixture, which needs a
// *testing.T to load.
func allFixtures(t *testing.T) []st.Fixture {
	t.Helper()
	var steps []st.Step
	for _, m := range st.LoadManifest(t, filepath.Join("..", "..", "testdata", "real-shape")) {
		steps = append(steps, m)
	}
	steps = append(steps, st.Run{}, st.Run{})
	return append(parityFixtures, st.Fixture{Name: "real shape", Steps: steps})
}

// restow runs dfm's planner and applier on a layout and returns the plan.
func restow(t *testing.T, l st.Layout) *stow.Plan {
	t.Helper()
	plan, err := tryRestow(l)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// tryRestow is restow without the *testing.T, safe to call from a goroutine.
func tryRestow(l st.Layout) (*stow.Plan, error) {
	plan, err := stow.Restow(l.Root, l.Target, stow.Packages)
	if err != nil {
		return nil, fmt.Errorf("dfm restow: %w", err)
	}
	if len(plan.Conflicts) == 0 {
		if err := plan.Apply(nil); err != nil {
			return nil, fmt.Errorf("dfm apply: %w", err)
		}
	}
	return plan, nil
}

// recoverStow asserts that --restow died the way StowDies promises, then
// runs a plain stow so the layout reflects what stow would have produced.
func recoverStow(t *testing.T, l st.Layout, exit int, out string) (int, string) {
	t.Helper()
	if exit == 0 || !strings.Contains(out, "ERROR: unstow_contents() called with invalid target") {
		t.Fatalf("expected stow --restow to die here, got exit %d:\n%s", exit, out)
	}
	return st.RunStowPlain(l)
}

// TestParity runs every fixture against stow and dfm in two separate
// layouts and diffs the resulting target trees at each st.Run.
func TestParity(t *testing.T) {
	st.RequireStow(t)
	for _, fx := range allFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()
			stowL, dfmL := st.NewLayout(t), st.NewLayout(t)
			for i, step := range fx.Steps {
				switch s := step.(type) {
				case st.Mutation:
					s.Apply(t, stowL)
					s.Apply(t, dfmL)
				case st.Run:
					var (
						wg       sync.WaitGroup
						stowExit int
						stowOut  string
						plan     *stow.Plan
						dfmErr   error
					)
					wg.Add(2)
					go func() { defer wg.Done(); stowExit, stowOut = st.RunStow(stowL) }()
					go func() { defer wg.Done(); plan, dfmErr = tryRestow(dfmL) }()
					wg.Wait()
					if dfmErr != nil {
						t.Fatalf("step %d: %v\nstow said:\n%s", i, dfmErr, stowOut)
					}
					if s.StowDies {
						stowExit, stowOut = recoverStow(t, stowL, stowExit, stowOut)
					}

					if (stowExit != 0) != (len(plan.Conflicts) > 0) {
						t.Fatalf("step %d: stow exit %d (%s) but dfm conflicts %v", i, stowExit, stowOut, plan.Conflicts)
					}
					if d := st.Diff(st.Snapshot(t, stowL), st.Snapshot(t, dfmL)); d != "" {
						t.Fatalf("step %d: trees differ (-stow +dfm):\n%s\nstow said:\n%s", i, d, stowOut)
					}
					if isRepeatRun(fx.Steps, i) && len(plan.Actions) != 0 {
						t.Errorf("step %d: repeat run planned %d actions, want 0: %+v", i, len(plan.Actions), plan.Actions)
					}
				default:
					t.Fatalf("bad step %T", step)
				}
			}
		})
	}
}

// isRepeatRun reports whether step i is a st.Run directly after another st.Run,
// i.e. nothing changed and the plan must be empty.
func isRepeatRun(steps []st.Step, i int) bool {
	if i == 0 {
		return false
	}
	_, ok := steps[i-1].(st.Run)
	return ok
}

// TestParity_StowThenDfm stows with stow only, then runs dfm once: the plan
// must be empty and the tree unchanged. Fixtures whose final state is a
// conflict must be a conflict for dfm too.
func TestParity_StowThenDfm(t *testing.T) {
	st.RequireStow(t)
	for _, fx := range allFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()
			l := st.NewLayout(t)
			stowExit := 0
			for _, step := range fx.Steps {
				switch s := step.(type) {
				case st.Mutation:
					s.Apply(t, l)
				case st.Run:
					var stowOut string
					stowExit, stowOut = st.RunStow(l)
					if s.StowDies {
						stowExit, _ = recoverStow(t, l, stowExit, stowOut)
					}
				}
			}
			before := st.Snapshot(t, l)
			plan := restow(t, l)
			if stowExit != 0 {
				if len(plan.Conflicts) == 0 {
					t.Fatalf("stow ended in conflict but dfm planned %+v", plan.Actions)
				}
				return
			}
			if len(plan.Actions) != 0 {
				t.Errorf("dfm after stow planned %d actions, want 0: %+v", len(plan.Actions), plan.Actions)
			}
			if d := st.Diff(before, st.Snapshot(t, l)); d != "" {
				t.Errorf("dfm after stow changed the tree:\n%s", d)
			}
		})
	}
}

// TestParity_DfmThenStow is the reverse: dfm builds the tree, then stow
// must find nothing to do.
func TestParity_DfmThenStow(t *testing.T) {
	st.RequireStow(t)
	for _, fx := range allFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()
			l := st.NewLayout(t)
			var plan *stow.Plan
			for _, step := range fx.Steps {
				switch s := step.(type) {
				case st.Mutation:
					s.Apply(t, l)
				case st.Run:
					plan = restow(t, l)
				}
			}
			before := st.Snapshot(t, l)
			stowExit, stowOut := st.RunStow(l)
			if len(plan.Conflicts) > 0 {
				if stowExit == 0 {
					t.Fatalf("dfm ended in conflict but stow succeeded:\n%s", stowOut)
				}
				return
			}
			if stowExit != 0 {
				t.Fatalf("stow after dfm failed:\n%s", stowOut)
			}
			if d := st.Diff(before, st.Snapshot(t, l)); d != "" {
				t.Errorf("stow after dfm changed the tree:\n%s", d)
			}
		})
	}
}
