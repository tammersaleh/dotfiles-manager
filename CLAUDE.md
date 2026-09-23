# dotfiles-manager

`dfm` manages `~/dotfiles`: it replaces the `bin/dotfiles` bash script in the
public dotfiles repo and GNU Stow with one Go binary. The binary is `dfm`; the
repository and Go module are `dotfiles-manager`. Go, Kong, non-interactive.

`SPEC.md` is the source of truth for behavior and the output contract.
`README.md` is user-facing usage. `skills/dotfiles-manager/SKILL.md` is the
agent-facing skill. The sibling repos at `~/src/github.com/tammersaleh/slack-cli`,
`confluence-cli`, and `lattice-cli` are the reference implementations for
tooling, workflow, and release mechanics; when this file is silent, do what
they do.

## Status

Stub, scaffolded 2026-09-23. Nothing implemented. Start on SPEC.md's open
questions, then `dfm install` with the stow parity suite.

## The system being replaced

Read these before writing code; they are the behavior to reproduce:

- `~/dotfiles/public/bin/dotfiles` - the bash script (install, public,
  private, ignore, pull).
- `~/dotfiles/public/.stow-local-ignore` and the private repo's copy.
- GNU Stow 2.4.1 (`/opt/homebrew/bin/stow`), used as
  `stow --dir=$HOME/dotfiles --target=$HOME --restow public private`.
- `~/dotfiles/public/post-pull.sh`, run by `pull`.

Never test against the real `$HOME`. Use `--root`/`--target` on a temp tree.
Running `dfm install` for real is the final verification step of the first
release, after the parity suite passes, and only with `--dry-run` first.

## This repo is public

Paths under `~/dotfiles/public` may be named. Nothing from `~/dotfiles/private`
appears here: not file names beyond the ones already visible in `SPEC.md`
(`.config`, `.claude`, `.aws`, `.agents`, `bin`), not contents, not hostnames.
No employer name anywhere. Fixtures use synthetic file names (`.examplerc`,
`.config/example/config.toml`) and `alice` as the user.

## Design constraint: drop-in parity

The first release reproduces today's symlink tree exactly. Anything stow does
that `dfm` does differently is a bug unless SPEC.md `## Decisions` says
otherwise. New behavior is a SPEC.md decision first.

## Output and error contract

Human-readable progress on stderr by default; `--json` gives the sibling
JSONL contract (rows, then a `_meta` trailer). Fatal errors are one JSON
object on stderr with `error` (stable snake_case code), `detail`, `hint`, and
`path` where relevant. Full detail in `SPEC.md`.

### Exit codes

- `0` success
- `1` general error
- `2` conflict: target exists and is not ours; nothing changed
- `3` reserved
- `4` git or network error

## Workflow

Work is driven by `SPEC.md`. Every change - feature, bug fix, perf fix,
refactor - follows the same workflow. No shortcuts for "small" fixes:

1. Read `SPEC.md` for the relevant command/feature.
2. Create a feature branch off main (or work directly on main for hot fixes -
   still follow every other step).
3. Red-green-refactor: write failing tests first, then implement, then clean up.
4. Run `mise run check` (test + lint + build) after every change. It must pass
   before committing. Gate on its exit code directly; piping it through `grep`
   masks a nonzero exit.
5. Keep commits small and conventional. Commit type drives releases - see
   "Release versioning".
6. MANDATORY code review before push (code changes only; skip when every
   modified file is Markdown or plain-text documentation): spawn a
   `feature-dev:code-reviewer` sub-agent on the pending changes (`git diff
   main...HEAD` for a branch, or the commits about to push for direct-to-main
   work). Tell the reviewer to scrutinize tests: tests that don't test what
   they claim, useless tests, missing coverage. Address every important or
   critical finding, then re-run the reviewer to confirm the fixes are clean.
   Never push code without a clean review pass.
7. Merge to main and push. The pre-push hook runs `mise run check`; never
   bypass with `--no-verify`.
8. Not done until installed and verified locally. After a release-cutting
   push, wait for the tag, install the cask (see "Distribution"),
   confirm `dfm version` is the version just cut, and exercise the new
   behavior with the installed binary (`--dry-run` against the real root
   first). A background `Bash` poll loop (`run_in_background`, `sleep 90`)
   works; a sub-agent poller bails early. Skill changes ship via
   `skills update`; confirm the expected text is in the installed skill.
9. Retrospective: update CLAUDE.md with anything learned that would help a
   future session.
10. Move on to the next feature.

Never ask permission to run this workflow. Committing, pushing to main, and
waiting out the release are the documented process, not a decision point.

## Bug reports and todos

`bugs/` and `todo/` are ignored scratch space holding work orders, not
artifacts. Delete the file when the work lands - findings belong in CLAUDE.md,
SPEC.md, and the commit body. `todo/README.md` describes the house style.

- `bugs/` - something is broken now. Verify, fix, delete.
- `todo/` - work deferred out of the current change. Written for a session
  starting cold: what was measured and how, the proposed approach, and what
  to verify rather than assume. Verbatim commands and output.

Verify before fixing, and verify the whole report. Silent-wrong-answer
behavior (exit 0 with a wrong symlink tree) is the most valuable kind to write
down.

## Release versioning

Releases are automated via release-please + GoReleaser. Release-please watches
main; a version-bumping commit opens a release PR that auto-merges once CI is
green; the merge cuts a tag and GitHub Release; GoReleaser builds binaries and
updates the Homebrew cask. Nobody runs `git tag` by hand and nobody clicks
Merge on the release PR.

Commit type is the release trigger, not a style choice:

- `feat:` minor bump. New commands, flags, outputs.
- `fix:` patch bump. Behavior that was promised but broken.
- `feat!:` / `BREAKING CHANGE:` footer. Anything that breaks a caller:
  removed or renamed flags, changed output shape, exit codes, behavior.
- `chore:`, `docs:`, `test:`, `refactor:`, `perf:`, `style:` - no release.

Rules: one type per commit (split mixed intent); never downgrade a type to
avoid a release or upgrade one to force it; imperative subject under ~70 chars
(release-please quotes it verbatim). The version number is never a reason to
ask. Pick the type, say in one line why, push.

Pushing a non-releasing commit to main after the release PR opens leaves that
PR `BEHIND` and auto-merge never fires. Either push cleanup before the
`feat:`/`fix:`, or when `gh pr view N --json mergeStateStatus` says `BEHIND`,
run `gh pr update-branch N`.

The `RELEASE_PAT` repository secret (user-attributed PAT with `repo` scope,
shared with the siblings) comes from the "Github Release Automation Token"
item in the personal 1Password vault. If it ever needs resetting: `op item get`
must carry `--account my.1password.com` (two accounts are signed in and an
unscoped call returns nothing on stdout), and `gh secret set` accepts empty
stdin without complaint, so check the value's length before piping. An empty
secret fails as `Input required and not supplied: token`.

## Distribution

Homebrew cask `tammersaleh/tap/dotfiles-manager` (`Casks/dotfiles-manager.rb`
in `tammersaleh/homebrew-tap`), written by GoReleaser on each release. The
Brewfile line is `cask 'tammersaleh/tap/dotfiles-manager'` in
`~/dotfiles/public/packages/Brewfile`; `~/packages/go` installs and upgrades
it. Until the first release the Brewfile line is absent because the cask does
not exist yet; add it with the first `feat:`.

"Installed and verified" in the Workflow means `brew upgrade
tammersaleh/tap/dotfiles-manager` (or `~/packages/go dotfiles-manager`) of the
tag just cut, then `dfm version` matching it.

Chicken-and-egg: `dfm` will one day remove `brew 'stow'` from the Brewfile,
and `~/packages/go` runs from a stowed symlink. Keep stow in the Brewfile
until `dfm install` has run cleanly on this machine at least once.

## Autonomy

Work through features independently. Never stop to ask "should I continue?" -
the answer is always yes. After a status summary, keep working. Escalate only
when a design decision is not covered by `SPEC.md`, or something feels wrong
(scope creep, a stow behavior the spec did not anticipate, anything that
would touch the real `$HOME` outside the documented verification step).

Neither exception covers releases. Commit type, version bump, pushing to main,
and cutting a release are workflow, never questions.

## Testing

Tests live next to the code they test (`foo_test.go`). Table-driven tests.
Filesystem tests use `t.TempDir()` for root and target. The stow parity suite
shells out to real `stow` when it is on `PATH` and skips otherwise, so CI
(ubuntu, no stow) runs the pure-Go cases and the local machine runs parity.
Git tests use a bare repo in a temp dir as the remote; nothing touches
GitHub. Never read or write the real `$HOME` or `~/dotfiles` in a test.

golangci-lint is v2. `errcheck` is disabled for `_test.go` only
(`.golangci.yml`). gopls "modernize" hints are not part of the gate.

## Git

Personal project. Commit on a branch or directly on main, merge, push. Don't
open pull requests; `gh pr create` is not part of any flow here. The only PR is
the release-please automation PR.

The SSH remote key is fingerprint-gated, so a non-interactive `git push origin`
over SSH fails. Push over HTTPS with the gh credential helper (`gh auth
setup-git` once, then `git push
https://github.com/tammersaleh/dotfiles-manager.git main:main`). Exception:
changes under `.github/workflows/` need a token with the `workflow` scope;
push those over SSH with the fingerprint. Because release-please's merge
advances remote main, fetch and rebase over HTTPS before each subsequent push:

```bash
git fetch https://github.com/tammersaleh/dotfiles-manager.git main && git rebase FETCH_HEAD
```

## Sandbox

GPG-signed commits and `mise run` commands require
`dangerouslyDisableSandbox: true` (Go build cache and GPG keyring access).

## Project structure (target)

```
cmd/
  root.go        # CLI struct, global flags (--root, --target, --dry-run, --json)
  install.go     # install
  adopt.go       # public / private
  ignore.go      # ignore
  pull.go        # pull
  status.go      # status
  version.go
  dfm/           # main
internal/
  stow/          # ignore lists, planner (fold/unfold/refold), conflicts, applier
  gitx/          # shell-out wrapper: stash, fetch, rebase, status
  output/        # human and JSONL printers, Error, Meta
skills/dotfiles-manager/SKILL.md
```
