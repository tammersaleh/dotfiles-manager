# dotfiles-manager

`dfm` manages a two-repo dotfiles setup (`~/dotfiles/public`,
`~/dotfiles/private`) symlinked into `$HOME`. It replaces the `bin/dotfiles`
bash script and GNU Stow with one binary that produces the same symlink tree.

Status: stub. See `SPEC.md`.

## Install

```bash
brew install tammersaleh/tap/dotfiles-manager
```

## Use

```bash
dfm install              # restow public then private into $HOME; idempotent
dfm public .examplerc    # move a file from $HOME into the public repo and relink
dfm private .config/x    # same, private repo
dfm ignore .cache/x      # add /.cache/x to the public .gitignore
dfm pull                 # update both repos, relink, run post-pull hooks
dfm status               # dirty/ahead/behind per repo, pending conflicts
```

Every mutating command takes `--dry-run`. `--json` switches stdout to JSONL.

## Agent skill

```bash
skills add tammersaleh/dotfiles-manager -g
```

## Development

```bash
mise run setup-hooks   # once: pre-push runs mise run check
mise run check         # test + lint + build
```
