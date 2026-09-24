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
  and readable. `fresh-install.sh` stays a shell script for now; see
  `## Planned: dfm bootstrap`.

## Layout (fixed, matches today)

```
~/dotfiles/            root   (stow --dir)
~/dotfiles/public/     package "public"  - git repo tammersaleh/dotfiles-public
~/dotfiles/private/    package "private" - git repo tammersaleh/dotfiles-private (git-crypt)
$HOME                  target (stow --target)
```

The package list is hardcoded: `public` then `private`, always in that order.
There is no flag to add packages. Root and target are
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

Append `/<path>` to the `.gitignore` of the package that owns `<path>`.
`<path>` resolves like `public`/`private` (relative to `$HOME`, or absolute).
Ownership is decided by following the `$HOME` symlink chain for the path or
its nearest linked ancestor into `~/dotfiles/<package>/`. If no package owns
it: error `not_tracked`, exit 1. No flag overrides the choice. If the line is
already present, say so and exit 0 without appending.

The script only ever wrote the public `.gitignore`; autodetection is a
recorded decision.

### `dfm pull`

First check every package with `git status --porcelain --untracked-files`. If
any package is dirty, error `dirty_tree` naming every dirty package, exit 1,
nothing fetched. There is no `--autostash`.

Then for each package, in order: `git fetch --all --quiet`, `git rebase
FETCH_HEAD --quiet`, then print `git status --short --untracked-files`.

The script stashed, rebased, and popped per package. That is dropped: a
failed pop leaves the tree conflicted, and the second repo would be pulled
after the first had already failed.

Then `install`, then run `post-pull.sh` from each package if present and
executable, public first, with `cwd` set to the package. `--no-hooks` skips
the hooks; `post-pull.sh` installs packages and is slow. A non-zero hook exit
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
- `1` general error (bad arguments, `already_tracked`, `not_tracked`,
  `dirty_tree`, `hook_failed`, `bad_ignore_pattern`)
- `2` conflict: a target path exists and is not ours; nothing was changed
- `3` reserved (siblings use it for rate limits; kept for consistency)
- `4` git or network error

## Testing

Never `$HOME`, never `~/dotfiles`. Every test builds its own root and target
under `t.TempDir()`. Four layers.

### Step driver

Fixtures are sequences, not trees. A fixture is `{name, steps []step}` where a
step is a mutation to the root or target (add file, delete file, add a
package file over a folded dir, drop a regular file in the target) or a run.
A run is a comparison point. Refold, dangling-link cleanup, and idempotence
all need a run, a mutation, and another run; a single-tree fixture cannot
express them. The same driver serves layers 1 and 2. Fixture cases run under
`t.Parallel()`; nothing is shared between them.

### Layer 1: stow parity

Two temp dirs per fixture. Stow owns one, `dfm` owns the other, from the
first step. Each mutation is applied to both dirs, then both tools run (in
parallel via `errgroup`), then the two targets are walked on disk and every
`(relative path, link text)` pair and every regular file is diffed. A
divergence fails at the step it appears.

Cases: single-package fold; two packages populate one dir and it unfolds;
refold when the second owner's file is deleted; symlink committed inside a
package; ignore-list exclusion with `.stow-local-ignore` present and absent;
conflict with a pre-existing regular file; dangling link after a package
file is deleted; `dfm` twice yields zero actions on the second run. Cross-tool
no-op cases run both tools on one dir in sequence: stow then `dfm` reports
zero actions and leaves the tree unchanged, and the reverse.

Stow is `stow --dir=<root> --target=<target> --restow public private`,
version 2.4.1 exactly. The suite skips, loudly, when `stow` is not on `PATH`
or `stow --version` is not 2.4.1. CI builds 2.4.1 from the GNU tarball
(`./configure && make && sudo make install`, Perl only, seconds) with the
version in one workflow variable, so CI never skips. Locally the suite skips
once `brew 'stow'` leaves the Brewfile.

This layer is scaffolding for the first release. When `dfm` deviates from
stow on purpose, the affected fixtures move to layer 2 with hand-written
expectations and a `## Decisions` entry. When the last one moves, delete the
suite and the CI stow install. Layer 2 is permanent; layer 1 exists to seed
its expected trees with stow's actual behavior rather than a reading of the
manual.

### Real-shape fixture

`testdata/real-shape/` is a committed fixture with the shape of the real
`~/dotfiles` and redacted names. A dev-only generator
(`go run ./internal/tools/shapegen`, never run by tests) walks both real
repos and builds one consistent rename map, so a path both packages populate
gets the same fake name in both. Preserved: directory tree shape and depth;
which package owns each path and where they overlap; symlinks inside a
package with targets remapped; the executable bit; both `.stow-local-ignore`
lists with any pattern naming a file rewritten through the map. Redacted:
every file and directory name below the top level, in both packages. The
top-level names already in this document stay. Contents are always empty.

Regenerate by hand when the real layout changes enough to matter. Before
committing, review the `git diff` and grep the output for anything resembling
a hostname, an email, an employer name, or a customer: a directory name can
be as sensitive as a file name, and the top-level exemption is the place a
leak would slip through.

### Layer 2: behavioral tests

Same driver and fixtures, no stow, expectations hand-written as `(relative
path, link text)` lists plus the expected action counts. Runs everywhere.
Covers what stow cannot verify: exit codes (`0`; `1` with `already_tracked`,
`not_tracked`, `dirty_tree`, `bad_ignore_pattern`, `hook_failed`; `2` on
conflict; `4` on git failure); `--dry-run` prints the plan and leaves the
tree byte-for-byte unchanged; `--json` shape, one object per action then
`_meta`; fatal errors are exactly one JSON object on stderr with `error`,
`detail`, `hint`, `path`; every conflict is reported before exit and nothing
changed; `public`/`private` path resolution, the inside-`$HOME` check, the
already-in-`~/dotfiles` refusal, and the resulting tree; `ignore` ownership
detection and the already-present case.

Most tests drive `cmd` in-process with `--root`/`--target` and captured
stdout/stderr so coverage counts them. A few run the built binary to prove
exit codes are real process exit codes.

### Layer 3: git

`git init --bare` in the temp dir is the remote. Clone it into
`<root>/public` and `<root>/private`, commit a seed tree, push. A second
clone plays another machine: commit there, push, and `dfm pull` has
something to fetch. No network, no GitHub. `GIT_CONFIG_GLOBAL=/dev/null`,
`HOME` set to the temp dir, author identity via env vars, so the real
gitconfig, signing key, and hooks never leak in. `git` is on every runner,
so nothing skips.

Cases: clean trees and remote ahead, both rebase, `install` runs, tree
reflects new files; private dirty and public clean, `dirty_tree` names
private, exit 1, public HEAD did not move; both dirty, error names both; an
untracked file counts as dirty; `post-pull.sh` runs public then private with
`cwd` set to the package, proven by the hook writing `$PWD` to a file;
public hook fails, `hook_failed` carries package and exit code, private hook
never runs; hook present but not executable is skipped; `--no-hooks` runs
neither; origin pointed at a nonexistent path exits 4.

### Layer 4: release gate, by hand

Once per release, against the real machine. Never automated; the only thing
that touches the real `$HOME` is a person watching.

1. Install the cask. `dfm version` matches the tag.
2. Snapshot the link tree. Before `dfm` is trusted for anything:

   ```sh
   find ~ -maxdepth 4 -path ~/Library -prune -o -path ~/src -prune -o -type l -lname '*/dotfiles/*' -exec ls -l {} + | sort > before.txt
   ```

   The unpruned scan takes 19s on this machine and picks up mise state and
   `Library` links that are not stow's; pruned it takes 0.4s and returns the
   127 links `install` owns. From the first release on, `dfm status --json`
   is the snapshot tool.
3. `dfm install --dry-run`. Expect zero planned actions. Any action is a
   parity bug; stop and investigate.
4. `dfm status`. Expect no conflicts and no broken links.
5. `dfm install`.
6. Snapshot again, `diff before.txt after.txt`. Expect empty.

Remediation if step 5 breaks the tree, valid for every release and needing
neither stow nor a prior `dfm`: `before.txt` has every owned link and its
target. Remove each link that points into `~/dotfiles`, then recreate the
snapshot:

```sh
awk '{print $9, $11}' before.txt | while read -r link target; do rm -f "$link"; ln -s "$target" "$link"; done
```

Fallbacks: GitHub Releases keeps every tarball, so the previous `dfm` is one
`curl` away, and `dfm status` on the broken version still reports what is
dangling even when `install` is what is wrong. Stow leaves the Brewfile only
after this gate passes for the first release.

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
- 2026-09-23: `ignore` autodetects which package owns the path and writes
  that package's `.gitignore`. No `--public`/`--private` override.
- 2026-09-23: `pull` refuses on a dirty tree. Both packages are checked before
  either is fetched. No `--autostash`.
- 2026-09-23: Package list is hardcoded (`public`, `private`). No `--package`
  flag; a third package is a `feat:`.
- 2026-09-23: Stow's `--dotfiles` mode (`dot-foo` -> `.foo`) is not supported.
  Verified: `rg -l --hidden -g '!.git' '^dot-|/dot-'` and `fd -H -u dot-`
  over both repos return nothing.
- 2026-09-23: Distributed as Homebrew cask `tammersaleh/tap/dotfiles-manager`.
  Add the Brewfile line with the first release.
- 2026-09-23: `pull --no-hooks` skips `post-pull.sh`.
- 2026-09-23: Stow parity suite is first-release scaffolding, CI-only once
  stow leaves this machine; deleted once `dfm` intentionally deviates.
- 2026-09-23: `fresh-install.sh` stays a shell script in the first release.
  `dfm bootstrap` is planned for a second release; see below.
- 2026-09-23: Argument parse errors are fatal `invalid_arguments`, exit 1.
- 2026-09-23: `--verbose` wins over `--quiet`. `--json` also suppresses
  progress lines.
- 2026-09-23: `dfm version` human output is the bare version on stdout;
  `--json` gives `{"version":...}` then `_meta`.
- 2026-09-23: Stow 2.4.1 `--restow` dies (`unstow_contents() called with
  invalid target`, exit 2, nothing changed) when a second package newly
  contributes to a directory the first has folded. `dfm` unfolds instead.
  Parity fixtures mark those steps as stow-dies and recover stow with a
  plain `stow`.
- 2026-09-23: Stow dies when the target has a real directory where the
  package has a file. `dfm` reports a conflict, exit 2.
- 2026-09-23: Refold never happens under restow of both packages: unstow of
  X folds only when every remaining link is Y's, and X's stow phase unfolds
  again. A directory that was unfolded stays a real directory. `dfm` matches
  stow; the refold path exists but is a no-op in restow.
- 2026-09-23: A fresh two-package install reports every shared directory as
  unfolded (public folds, private unfolds in the same plan). Accurate.
- 2026-09-23: Ownership is stow's textual check: link text joined with the
  link's directory has prefix `<root>/`. An absolute symlink into the root is
  a conflict on stow and ignored on unstow. Not "resolved target inside
  root" as written above.
- 2026-09-23: Dangling links inside a directory the package no longer has at
  all are never cleaned; stow only visits target directories that exist in
  the package.
- 2026-09-23: Link text is one `..` per level below the target:
  `~/.config/nvim/init.lua -> ../../dotfiles/public/.config/nvim/init.lua`.
  The example under "Relative link targets" has one `..` too many.
- 2026-09-24: `status` exits 2 when any conflict exists (as `install`
  would), 4 on `git_failed`, 1 on `package_missing`, else 0. Dirty, ahead,
  behind, broken links, no upstream, and detached HEAD are states, not
  errors.
- 2026-09-24: `status` never fetches. Ahead/behind is against the local
  tracking ref. `pull` owns the network.
- 2026-09-24: `status --json` rows: `{"kind":"package","package","dirty",
  "changes","upstream","ahead","behind"}` (upstream/ahead/behind null with no
  upstream), `{"kind":"conflict","package","path","detail","hint"}`,
  `{"kind":"broken_link","package","path","target"}`, then `_meta` with
  `dirty`, `conflicts`, `broken_links`, `pending` (omitted when zero).
  `pending` counts every planned install action. Rows are buffered so a git
  failure leaves stdout empty.
- 2026-09-24: `status` human output goes to stdout (it is the result), one
  line per package, then conflicts, broken links, and a one-line install
  summary. `--quiet` does not suppress it.
- 2026-09-24: `gitx` inherits the user's gitconfig in production (so
  `core.excludesFile` is honored and globally ignored files are not dirty)
  and sets only `GIT_TERMINAL_PROMPT=0` and `GIT_OPTIONAL_LOCKS=0`. Tests
  isolate via `gittest.Isolate`.
- 2026-09-24: `changes` is the porcelain line count with
  `--untracked-files=all`, one line per file.

## Planned: `dfm bootstrap`

Second release, after `install` parity is proven on this machine.

Today `fresh-install.sh` runs on a bare machine: installs Homebrew, then
`stow git git-lfs git-crypt bash`, clones both repos with a temporary GitHub
token, runs `git lfs checkout` in public and `git-crypt unlock ~/key` in
private, deletes the stray `~/.gitconfig` LFS creates, runs `dotfiles
install`, then `~/packages/go`. A failure mid-script leaves a half-set-up
machine and a clear-text token in shell history.

`dfm bootstrap --token ... --key ~/key` replaces everything after Homebrew is
installed, with real error handling, `--dry-run`, and idempotent re-runs. A
short `curl` line installs Homebrew and `dfm`; something has to fetch `dfm`
before `dfm` can run, so a shell stage exists either way. Homebrew install
itself is not absorbed.

