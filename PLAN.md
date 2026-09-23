# Implementation plan

Drives the first release of `dfm` from stub to installed. `SPEC.md` is the
behavior contract; `CLAUDE.md` is the workflow. This file is the resumable
state: what is done, what is next, and decisions made along the way. Update
inline as work lands.

## Ground rules for this run

- Each feature runs in its own sub-agent (CLAUDE.md, "One sub-agent per
  feature"). The main thread updates this file, reads the agent report, does
  install verification and the retrospective.
- Never run `dfm` against the real `$HOME` or `~/dotfiles`. The layer 4
  release gate (SPEC.md "Testing") needs Tammer's explicit approval per step,
  `--dry-run` first. This is a standing instruction from Tammer, 2026-09-23.
- Codex MCP failed to connect in the session that started this plan. Codex
  planning and review are skipped until it reconnects; `feature-dev:code-reviewer`
  is the review gate meanwhile. Note when Codex is back and use it.
- Every release-cutting push waits for the tag and installs the cask before
  the next feature starts.

## Feature order

Each entry is one sub-agent run and one release (or none for `chore:`).

1. [ ] `feat: cli skeleton with version` - Kong root with global flags
   (`--root`, `--target`, `--dry-run`, `--json`, `--verbose`, `--quiet`,
   env `DFM_ROOT`/`DFM_TARGET`), `dfm version` from build info or ldflags,
   `internal/output` (human stderr progress, JSONL rows plus `_meta`
   trailer, fatal `Error{error,detail,hint,path}` on stderr, exit codes).
   All other commands registered as stubs that exit 1 `not_implemented`.
   Adds `cask 'tammersaleh/tap/dotfiles-manager'` to
   `~/dotfiles/public/packages/Brewfile` (commit and push that dotfiles
   change too). Cuts 0.1.0.
2. [ ] `feat: dfm install` - `internal/stow`: ignore lists (Perl to RE2,
   stow default list, `bad_ignore_pattern`), planner (fold, unfold, refold,
   dangling-link removal, relative link text, package symlinks as files),
   conflict detection (exit 2, all conflicts reported, nothing changed),
   applier, `--dry-run`. Step-driver test harness. Layer 1 parity suite
   (skips unless `stow --version` is 2.4.1). Layer 2 behavioral tests for
   install. CI builds stow 2.4.1 from tarball. `testdata/real-shape` with
   `internal/tools/shapegen` (agent reviews the diff for leaks before commit).
3. [ ] `feat: dfm status` - read-only: per-package dirty/clean, ahead/behind,
   pending conflicts and broken links. Needs a minimal `internal/gitx`
   (status, rev-list). Layer 3 harness (bare remote in temp dir) starts here.
4. [ ] `feat: dfm public and private` - adopt path into a package, then
   install. Path resolution, inside-`$HOME` check, already-in-root refusal,
   `already_tracked`.
5. [ ] `feat: dfm ignore` - ownership via symlink chain, append `/<path>` to
   the owning package `.gitignore`, `not_tracked`, already-present no-op.
6. [ ] `feat: dfm pull` - dirty check across both packages first, fetch and
   rebase per package, install, hooks with `cwd`, `--no-hooks`,
   `hook_failed`, exit 4 on git failure. Full layer 3 cases.
7. [ ] Skill and README pass - `skills/dotfiles-manager/SKILL.md` and
   `README.md` describe the real surface. `docs:`, no release.
8. [ ] Layer 4 release gate, by hand, WITH TAMMER. Snapshot, `dfm install
   --dry-run` expecting zero actions, `dfm status`, `dfm install`, diff.
   Only after this: remove `brew 'stow'` from the Brewfile and add
   `alias dotfiles=dfm`.

## Sub-agent brief template

Give every feature agent:

- The feature number and this file's path.
- The SPEC.md sections to read (always "Stow semantics", "Output", "Exit
  codes", "Testing"; plus the command section).
- CLAUDE.md workflow steps 1 through 7, verbatim pointer.
- Siblings for conventions: `~/src/github.com/tammersaleh/slack-cli`
  (`cmd/root.go`, `internal/output`), `confluence-cli`, `lattice-cli`.
- The public-repo constraint: nothing from `~/dotfiles/private`, no employer
  name, synthetic fixtures.
- `mise run check` and GPG commits need `dangerouslyDisableSandbox: true`.
- Push over HTTPS: fetch and rebase `FETCH_HEAD` first.
- Report back: commits pushed, whether the commit type cuts a release,
  anything that belongs in `## Discoveries` below, anything SPEC.md did not
  cover.

## Discoveries

(Append as they come. Date each.)

## Decisions made during implementation

(Anything not already in SPEC.md `## Decisions`. Promote to SPEC.md when it
changes behavior.)
