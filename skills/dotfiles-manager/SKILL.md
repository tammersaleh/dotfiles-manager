---
name: dotfiles-manager
description: "Manage dotfiles with dfm: symlink the public and private dotfiles repos into $HOME (stow-compatible), track a new file publicly or privately, ignore a path, pull both repos. Load this BEFORE running any `dfm` command."
argument-hint: ""
allowed-tools:
  - Bash(dfm *)
---

# dfm

Stub. The command surface is in `SPEC.md` at
https://github.com/tammersaleh/dotfiles-manager; nothing is implemented yet.

Output contract (shared with the sibling CLIs): human-readable progress on
stderr, JSONL on stdout when `--json` is set, one object per line, then a
`_meta` trailer, always present: `{"_meta":{"has_more":false}}`. Fatal errors
are one JSON object on stderr with `error`, `detail`, and `hint`.

## Exit codes

`0` success, `1` general error, `2` conflict (a target path exists and is not
ours), `3` reserved, `4` git or network error.
