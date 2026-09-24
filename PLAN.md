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

1. [x] `feat: cli skeleton with version` (pushed 1f952e9, released v1.0.0, installed and verified 2026-09-23) - Kong root with global flags
   (`--root`, `--target`, `--dry-run`, `--json`, `--verbose`, `--quiet`,
   env `DFM_ROOT`/`DFM_TARGET`), `dfm version` from build info or ldflags,
   `internal/output` (human stderr progress, JSONL rows plus `_meta`
   trailer, fatal `Error{error,detail,hint,path}` on stderr, exit codes).
   All other commands registered as stubs that exit 1 `not_implemented`.
   Adds `cask 'tammersaleh/tap/dotfiles-manager'` to
   `~/dotfiles/public/packages/Brewfile` (commit and push that dotfiles
   change too). Cuts 0.1.0.
2. [x] `feat: dfm install` (eafb2f4, released v1.1.0 2026-09-24; ci commit c4fe0d3 awaits SSH push) - `internal/stow`: ignore lists (Perl to RE2,
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

- 2026-09-23: `mise run check` does not run `go mod tidy`; CI does. Run
  `go mod tidy && git status --short` before committing a dependency change
  and stage `go.sum` with `go.mod`.
- 2026-09-23: dotfiles-public default branch is `master`, remote is SSH and
  fingerprint-gated. Push with
  `git push https://github.com/tammersaleh/dotfiles-public.git HEAD:master`.
- 2026-09-23: release-please's first release from the scaffold manifest is
  1.0.0, not 0.1.0. Plan numbering assumed 0.1.0; irrelevant, moving on.
- 2026-09-23: `~/packages/go dotfiles-manager` fails with "not a brew
  package" when the cask is not yet installed. First install is
  `brew install --cask tammersaleh/tap/dotfiles-manager`; upgrades go through
  `~/packages/go dotfiles-manager`.
- 2026-09-23: GoReleaser's cask template emits a deprecated `postflight`
  block (Homebrew warns, install still works). Same in the siblings; fix
  belongs in the GoReleaser upgrade, not here.
- 2026-09-24: INCIDENT. `TestStubs_NotImplemented` ran `install` with no
  `--root`, so the first `go test ./cmd` after the command existed ran
  `dfm install` against the real `$HOME`. Zero actions, nothing changed.
  Fixed; rule added to CLAUDE.md Testing.
- 2026-09-24: `testdata/real-shape/` is a manifest (`manifest.tsv` plus
  per-package ignore lists), not a tree, so empty dirs and exec bits survive
  git. Regenerate with `go run ./internal/tools/shapegen`.
- 2026-09-24: Both real packages contain absolute symlinks. Stow tolerates
  them only because their parent dirs stay folded; a future overlap in those
  dirs surfaces as absolute-symlink conflicts.
- 2026-09-24: Parity tests run stow and dfm in goroutines; helpers return
  errors rather than take `*testing.T`. `sync.WaitGroup`, no errgroup.
- 2026-09-24: 34 parity fixtures x 3 drivers (side-by-side, stow-then-dfm,
  dfm-then-stow) = 102 subtests against stow 2.4.1 locally.
- 2026-09-24: `.github/workflows/` commits block an HTTPS push of everything
  above them. Tammer pushes those over SSH.
- 2026-09-23: `cmd.Run(args, stdout, stderr) int` is the in-process entry
  point. `--root`/`--target` resolve lazily via `cli.RootDir()`/`TargetDir()`
  so tests override with `t.Setenv("HOME", t.TempDir())`. Kong exit is
  recovered via a panic sentinel. `output.Printer` has Progress, Verbosef,
  Row, PrintMeta, Result, PrintError.

## Decisions made during implementation

(Anything not already in SPEC.md `## Decisions`. Promote to SPEC.md when it
changes behavior.)
