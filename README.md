# dotfiles-manager

`dfm` manages a two-repo dotfiles setup symlinked into `$HOME`. It replaces
the `bin/dotfiles` bash script and GNU Stow with one binary that produces the
same symlink tree. Non-interactive; every mutating command takes `--dry-run`.

`SPEC.md` is the behavior contract. `skills/dotfiles-manager/SKILL.md` is the
agent skill.

## Install

```bash
brew install --cask tammersaleh/tap/dotfiles-manager
```

Or with Go:

```bash
go install github.com/tammersaleh/dotfiles-manager/cmd/dfm@latest
```

## Layout

```
~/dotfiles/            root: --root, DFM_ROOT
~/dotfiles/public/     package "public", a git clone
~/dotfiles/private/    package "private", a git clone
$HOME                  target: --target, DFM_TARGET
```

The package list is fixed: `public` then `private`. Each package may carry a
`.stow-local-ignore` (one Perl regex per line); when absent, stow's default
ignore list applies. A package file `public/.examplerc` is linked as
`~/.examplerc -> ../dotfiles/public/.examplerc`. A directory only one
package populates is linked whole (folded); a directory both populate becomes
a real directory in the target with each child linked.

## Commands

### install

```bash
dfm install
```

Restows both packages into the target, the same as
`stow --restow public private`. Creates missing links, removes links whose
package file is gone, and unfolds a directory when the second package starts
populating it. Idempotent: a second run prints `nothing to do`.

Exit 2 when a target path exists and is not a dfm link, with one `conflict`
error per path and nothing changed.

### status

```bash
dfm status
```

Read-only. Per package: dirty or clean, ahead or behind the local tracking
ref (never fetches). Then every conflict and broken link the next `install`
would hit, and a one-line install summary. Output goes to stdout.

```
public: clean, in sync with origin/main
private: dirty (1 change), behind 1 of origin/main
broken link .awsrc -> ../dotfiles/private/.awsrc (private)
install would apply 1 action
```

Exit 2 when a conflict exists.

### public and private

```bash
dfm public .examplerc
dfm private .config/example
```

Move a path from the target into the package and run `install` so it is
linked back. A relative path resolves against the target; an absolute path
must be inside the target and outside the root. Directories move whole. A
path already linked from a package, or one that already exists in the
package, is `already_tracked`. A directory holding a dfm link anywhere below
it is `contains_tracked`; adopt its children instead.

`--dry-run` prints the move but not the install plan that follows it.

### ignore

```bash
dfm ignore .config/example/cache
```

Appends `/<package-relative path>` to the `.gitignore` of the package that
owns the path. Ownership follows the dfm symlink chain upward from the path;
a path with no dfm-owned ancestor is `not_tracked`. A line already present
is a no-op with exit 0. Does not run `install`.

### pull

```bash
dfm pull
dfm pull --no-hooks
```

Checks both packages for uncommitted changes first; any dirty package is
`dirty_tree`, exit 1, nothing fetched. Then per package: `git fetch --all`,
`git rebase FETCH_HEAD`. Then `install`. Then each package's `post-pull.sh`
when present and executable, public first, with the package as the working
directory. A hook that exits non-zero is `hook_failed` and the next hook does
not run. `--no-hooks` skips the hooks.

`--dry-run` still runs the dirty check, then prints what it would fetch, the
install plan against the current tree, and the hooks it would run.

### version

```bash
dfm version
```

## Global flags

| Flag | Meaning |
|------|---------|
| `--root DIR` | Dotfiles root. `DFM_ROOT`. Default `~/dotfiles`. |
| `--target DIR` | Directory linked into. `DFM_TARGET`. Default `$HOME`. |
| `--dry-run` | Print the plan, change nothing, exit 0. |
| `--json` | JSONL on stdout, one object per action, then `_meta`. Suppresses progress lines. |
| `--verbose` | Echo every filesystem and git operation to stderr. Wins over `--quiet`. |
| `--quiet` | Suppress progress lines. Errors and `status` output still print. |

## Output

Human mode prints progress to stderr and nothing to stdout, except `status`
(the report is the result), `version`, and hook output under `pull`.

`--json` prints one object per action on stdout, then a `_meta` trailer
whose counters are omitted when zero:

```jsonl
{"action":"link","package":"public","path":".examplerc","target":"../dotfiles/public/.examplerc"}
{"action":"unlink","package":"private","path":".awsrc","reason":"source_missing"}
{"action":"unfold","package":"public","path":".config"}
{"_meta":{"has_more":false,"created":1,"removed":1,"unfolded":1}}
```

Fatal errors are one JSON object on stderr in every mode, with `error` (a
stable snake_case code), `detail`, `hint`, and `path` where relevant:

```json
{"error":"conflict","detail":".examplerc exists and is not a dotfiles symlink","hint":"dfm public .examplerc to adopt it, or remove it and rerun","path":".examplerc"}
```

The skill lists every row shape and error code.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success, including `--dry-run` |
| 1 | General error: bad arguments, `already_tracked`, `not_tracked`, `dirty_tree`, `hook_failed`, `bad_ignore_pattern` |
| 2 | Conflict: a target path exists and is not ours. Nothing changed. |
| 3 | Reserved |
| 4 | Git or network error |

## Migrating from the bash script

`dfm` reads the same `.stow-local-ignore` files and produces the same links,
so a tree stowed by stow restows under `dfm` with zero actions. Verify before
trusting it:

```bash
dfm install --dry-run   # expect: nothing to do
dfm status              # expect: no conflicts, no broken links
```

Then `alias dotfiles=dfm`. Stow is no longer needed once `dfm install` has
run cleanly.

## Agent skill

```bash
skills add tammersaleh/dotfiles-manager -g
```

## Development

```bash
mise run setup-hooks   # once: pre-push runs mise run check
mise run check         # test + lint + build
```

Tests never touch `$HOME` or `~/dotfiles`. The stow parity suite runs when
`stow --version` is 2.4.1 and skips otherwise. See `CLAUDE.md` for the
workflow and release mechanics.
