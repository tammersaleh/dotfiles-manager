# dotfiles-manager Specification

`dfm` is a single Go binary that replaces two things: `~/dotfiles/public/bin/dotfiles`
(a bash script) and GNU Stow 2.4.1 as that script invokes it. The first release
must be a drop-in replacement: same layout on disk, same symlinks produced,
same commands, so `alias dotfiles=dfm` is the only migration step.

## Design principles

- **Behavioral parity first.** Every command below reproduces what the bash
  script plus stow do today. New behavior is a `## Decisions` entry, never a
  side effect.
- **Stow-compatible symlink semantics.** `dfm` reads the existing
  `.stow-local-ignore` files and produces the same symlink tree stow does,
  including tree folding. A repo stowed by stow and re-stowed by `dfm` must be
  a no-op, and vice versa. Verified by diffing `find ~ -type l -lname
  '*/dotfiles/*'` before and after.
- **Non-interactive.** No prompts. Conflicts are errors with a hint, never a
  question.
- **Dry run everywhere.** Every mutating command takes `--dry-run` and prints
  the plan.
- **Human output by default, `--json` for agents.** This tool is run by hand
  more than by agents, unlike the siblings. Progress lines on stderr; `--json`
  switches stdout to the sibling JSONL contract.
- **First release starts after clone.** `dfm` assumes both repos are cloned
  and readable. Whether it later absorbs `fresh-install.sh` (Homebrew,
  git-crypt, LFS) is open question 6.

## Layout (fixed, matches today)

```
~/dotfiles/            root   (stow --dir)
~/dotfiles/public/     package "public"  - git repo tammersaleh/dotfiles-public
~/dotfiles/private/    package "private" - git repo tammersaleh/dotfiles-private (git-crypt)
$HOME                  target (stow --target)
```

Package order is always `public` then `private`. Root and target are
overridable by `--root` and `--target` (and `DFM_ROOT`, `DFM_TARGET`) for
tests; the defaults are the paths above.

## Stow semantics `dfm` must reproduce

These are the parts of GNU Stow 2.4.1 the current setup depends on. Anything
stow does that is not listed here is out of scope until it shows up in a diff.

### Ignore lists

Each package may have a `.stow-local-ignore`, one Perl regex per line, blank
lines and `#` comments skipped. A path is ignored when the regex matches either
the basename or, if the pattern contains `/`, the package-relative path
anchored at the package root. When the file exists it fully replaces stow's
built-in default list; when absent, stow's defaults apply (`.git`, `.gitignore`,
`.gitattributes`, `.stow-local-ignore`, `README.*`, `LICENSE.*`, `COPYING`,
`.*\.swp`, and the rest of the list in stow's manual). `dfm` translates Perl
regex to Go RE2; the current files use nothing outside RE2. Lines that fail to
compile are a fatal `bad_ignore_pattern` error naming file and line.

Current public list, for reference and as a test fixture:

```
\.gitattributes
\.gitignore
\.git
tags.*
.*\.swp
\.envrc
README\.md
CLAUDE\.md
post-pull.sh
fresh-install.sh
\.claude/settings\.local\.json
```

### Tree folding

Stow links a whole directory when nothing else owns the target path: if
`~/.zsh` does not exist, `~/.zsh -> dotfiles/public/.zsh`. When two packages
both contribute under the same directory (both repos have `.config/`,
`.claude/`, `.aws/`, `.agents/`, `bin/`), the directory is a real directory in
`$HOME` and each child is linked individually. Folding happens at the deepest
level where a single package owns the subtree.

Restow must **unfold** when a second package starts contributing to a folded
directory (stow does this: replace the link with a real dir, relink the first
package's children one by one) and **refold** when a package stops
contributing and the directory is left holding links from a single package.
Refolding is where stow is subtle; verify against stow on a temp root before
trusting the implementation.

### Symlinks inside a package

A symlink in the package (`public/.nvim -> .config/nvim`) is linked as a file:
`~/.nvim -> dotfiles/public/.nvim`. Stow does not resolve it.

### Ownership and conflicts

A target path is "ours" when it is a symlink whose resolved target lies inside
the root. Anything else that exists where a link should go (a regular file, a
foreign symlink, a directory where a file link is wanted) is a conflict.
Stow's behavior is to plan everything, report every conflict, and change
nothing. `dfm` does the same and exits 2 with one `conflict` line per path and
a hint (`dfm public <path>` to adopt, or remove it).

### Restow

`stow --restow public private` is unstow then stow, for both packages, planned
as a single operation. Unstow removes links that point into the package and
prunes directories emptied by that removal (refolding included). Broken links
that point into the package (source deleted from the repo) are removed as
part of unstow; this is how a removed dotfile disappears from `$HOME` on
`dotfiles install`.

### Relative link targets

Links are relative, computed from the link's directory to the package file:
`~/.config/nvim/init.lua -> ../../../dotfiles/public/.config/nvim/init.lua`.

## Commands

The verb-first shape of the bash script is kept as-is.

### `dfm install`

Restow `public` then `private` into `$HOME`. Idempotent. Equivalent to
`stow --dir=$HOME/dotfiles --target=$HOME --restow public private`.

Exit 2 on any conflict, having changed nothing. Exit 0 with a summary of
links created, removed, unfolded, and refolded.

### `dfm public <path>` and `dfm private <path>`

Adopt a file or directory from `$HOME` into a package. `<path>` is relative to
`$HOME`. Today the script requires `$PWD == $HOME`; `dfm` instead resolves the
argument against `$HOME` if relative and against the filesystem if absolute,
then requires the result to be inside `$HOME`. It must not be inside
`~/dotfiles` already.

Steps: `mkdir -p` the parent inside the package, `mv` the path, then run
`install`. A directory is moved whole and becomes a folded link. Moving across
filesystems is not supported (`$HOME` and `~/dotfiles` share one).

If the destination already exists inside the package: error `already_tracked`,
exit 1, nothing moved.

### `dfm ignore <path>`

Append `/<path>` to `~/dotfiles/public/.gitignore`. That is all the script
does. If the line is already present, say so and exit 0 without appending.
Which repo's `.gitignore` is open question 1.

### `dfm pull`

For each package, in order: `git stash push -u --quiet -m "In dotfiles pull"`,
`git fetch --all --quiet`, `git rebase FETCH_HEAD --quiet`, `git stash pop
--quiet`, then print `git status --short --untracked-files`. The script also
touches and removes a `.force-stash` marker so the stash is never empty; `dfm`
detects "nothing to stash" directly and skips the pop.

Then `install`, then run `post-pull.sh` from each package if present and
executable, public first, with `cwd` set to the package. A non-zero hook exit
is a fatal `hook_failed` error with the package name and exit code; the
second hook still does not run (matches `set -e` in the script).

Git operations shell out to `git`; no go-git. Exit 4 when git fails to reach
the remote.

### `dfm status`

New, read-only. Report per-package: repo dirty or clean, ahead/behind the
remote, and any `$HOME` conflicts or broken links the next `install` would hit.
Zero side effects. This is the one addition to the surface in the first
release because every other command needs the same planning code and it gives
`--dry-run` a home.

### `dfm version`

Module version from build info, or the GoReleaser `ldflags` value when set.

## Global flags

- `--root <dir>` / `DFM_ROOT` (default `~/dotfiles`)
- `--target <dir>` / `DFM_TARGET` (default `$HOME`)
- `--dry-run` on every mutating command; prints the plan and exits 0
- `--json` JSONL on stdout, one object per action, then `_meta`
- `--verbose` echo every filesystem and git operation to stderr
- `--quiet` suppress progress lines (errors still print)

## Output

Default (human) mode writes progress to stderr and nothing to stdout except
what the wrapped commands print (`git status` in `pull`). `--json` mode:

```
$ dfm install --json
{"action":"link","package":"public","path":".zshenv","target":"dotfiles/public/.zshenv"}
{"action":"unlink","package":"private","path":".old-thing","reason":"source_missing"}
{"action":"unfold","package":"public","path":".config"}
{"_meta":{"has_more":false,"created":1,"removed":1,"unfolded":1,"refolded":0}}
```

Fatal errors are a single JSON object on stderr regardless of `--json`:

```json
{"error":"conflict","detail":".gitconfig exists and is not a dotfiles symlink","hint":"dfm public .gitconfig to adopt it, or remove it and rerun","path":".gitconfig"}
```

Conflicts are reported all at once, one line each, before exiting.

### Exit codes

- `0` success (including `--dry-run`)
- `1` general error (bad arguments, `already_tracked`, `hook_failed`,
  `bad_ignore_pattern`)
- `2` conflict: a target path exists and is not ours; nothing was changed
- `3` reserved (siblings use it for rate limits; kept for consistency)
- `4` git or network error

## Testing

Every symlink test runs against a temp root and temp target, never `$HOME`.
The parity suite builds a fixture tree, runs real `stow` (skipped when not on
`PATH`) and `dfm` on copies, and diffs the resulting link trees. Fixture cases:
single-package fold, two-package unfold, refold after removal, package-local
symlink, ignore-list exclusion, conflict with a regular file, broken link
cleanup.

## Decisions

- 2026-09-23: Repo is `tammersaleh/dotfiles-manager`, module
  `github.com/tammersaleh/dotfiles-manager`, binary `dfm`. Public.
- 2026-09-23: Verb-first command names kept from the bash script
  (`install`, `public`, `private`, `ignore`, `pull`) for drop-in parity.
- 2026-09-23: Stow is reimplemented, not shelled out to. `brew 'stow'` leaves
  the Brewfile once `dfm install` is verified on this machine.
- 2026-09-23: `public`/`private` drop the `$PWD == $HOME` requirement and
  resolve paths instead.
- 2026-09-23: `status` is the only new command in the first release.

## Open questions

Answer these in a fresh session before the first `feat:`. Each has the
context needed to decide without re-reading the bash script.

### 1. Which `.gitignore` does `ignore` write?

Today `dotfiles ignore <path>` appends `/<path>` to the public repo's
`.gitignore` only. Proposal: keep that default, add `--private` to target the
private repo instead.

### 2. Should `pull` stash over uncommitted work?

Today: `git stash push -u`, `fetch`, `rebase FETCH_HEAD`, `stash pop`, per
repo. The global git-hygiene rule forbids stashing or rebasing over
uncommitted work because a pop can leave files conflicted. Proposal: refuse
when the tree is dirty (exit 1, hint to commit), with `--autostash` restoring
today's behavior.

### 3. Hardcode the package list or discover it?

Today the script hardcodes `public` then `private`. Discovering every
directory under `~/dotfiles` that contains `.git` would also pick up
`~/dotfiles/doc`. Proposal: hardcode, allow repeated `--package` to add.

### 4. Does anything depend on stow's `--dotfiles` mode?

Stow's `--dotfiles` maps `dot-foo` in the package to `.foo` in the target.
The script does not pass it and both repos appear to use literal dot-names.
Confirm with `rg -l '^dot-|/dot-' ~/dotfiles/public ~/dotfiles/private`
before dropping support entirely.

### 5. Homebrew cask or `go install`?

The public siblings ship a cask from `tammersaleh/homebrew-tap`; the private
one uses `go install`. This repo is public, so the cask is the plan and
`.goreleaser.yml` already has the block. Confirm, then add
`cask 'tammersaleh/tap/dotfiles-manager'` to the Brewfile with the first
release.

### 6. Should `dfm` absorb `fresh-install.sh`?

The scaffold left fresh-install as a shell script. Reasons it was left out,
none of them decisive:

- Bootstrap ordering. `fresh-install.sh` runs on a bare machine before
  Homebrew exists. It installs Homebrew, then `stow git git-lfs git-crypt
  bash`, clones both repos with a temporary GitHub token, runs `git lfs
  checkout` in public and `git-crypt unlock ~/key` in private, deletes the
  stray `~/.gitconfig` LFS creates, runs `dotfiles install`, then
  `~/packages/go`. Something has to fetch `dfm` before `dfm` can run, so a
  shell (or `curl | sh`) stage exists either way.
- The steps are mostly shelling out to other tools (`brew`, `git`,
  `git-crypt`, `git lfs`), which a Go binary would wrap without adding
  behavior.
- Scope: "behave exactly like the current system" was read as the five
  `dotfiles` commands plus stow.

Reasons to absorb it anyway:

- The `curl` stage can be a one-liner that downloads the release tarball
  from GitHub, and `dfm bootstrap --token ... --key ~/key` does the rest with
  real error handling, `--dry-run`, and idempotent re-runs. Today a failure
  mid-script leaves a half-set-up machine and a clear-text token in shell
  history.
- One binary, one place to read what "install my machine" means.
- If `dfm` owns cloning, it can own the repo list and remote URLs, which
  question 3 also wants.

Options: (a) leave it as a shell script, (b) `dfm bootstrap` replaces
everything after Homebrew is installed, with a tiny `curl` line that installs
Homebrew and `dfm`, (c) full absorption including the Homebrew install.
Proposal: (b), as a second release after `install` parity is proven.
