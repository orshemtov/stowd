---
name: stowd
description: Manage dotfiles syncing with stowd, the GNU Stow file watcher. Use whenever the user mentions dotfiles, their dotfiles repo, stow/stowd, keeping config files in sync with $HOME, "why isn't my .config syncing", tracking or untracking a config into their dotfiles repo, running stowd as a macOS service, or checking stowd logs/status. Even if they don't name stowd explicitly — any request about mirroring a dotfile between $HOME and their dotfiles repo triggers this skill.
---

# stowd — Watch-and-Stow dotfiles

`stowd` watches a dotfiles repo (the *src*) and re-runs `stow` whenever files change, so config files in the repo are symlinked into your home directory (the *target*). It also has `track`/`untrack` commands for pulling real paths in `$HOME` into the repo.

## Prerequisites

- GNU Stow must be installed and on `PATH` (`stow` runs as a subprocess).
- `stowd` must be built and on `PATH`. Build from the repo: `go build` (Go ≥ 1.24), or `make install-user` for the service flow.
- Default src is `$STOWD_DOTFILES_FOLDER_PATH`, else `~/.dotfiles`. Default target is `$HOME`.

## Mental model (read this first)

The watcher is **one-directional**: it watches the **repo** and mirrors it into `$HOME` via symlinks. It does **not** watch `$HOME` for new files.

- **New file created directly in `$HOME` (e.g. `~/.config/foo/config.toml`)** is invisible to stowd — it has no counterpart in the repo, and `--adopt` does not grab brand-new paths. To sync it you must either:
  - **repo-first**: put it in the repo under `<src>/<package>/<relative-path>`, and the watcher symlinks it into `$HOME` within the debounce window (~2s for the launchd service); or
  - **home-first**: run `stowd dotfiles track <package> <path>` to move the real file into the repo and symlink it back.
- **Tracked files are symlinks** — editing the copy in `$HOME` writes straight through to the repo (same inode). When a tool replaces the symlink with a real file, the `--adopt` flag pulls that home-side content back into the repo on the next stow run.
- So "which side is source of truth?" — the repo is. `track` is the one-way valve for *adopting* existing home paths into the repo.

## Commands

### `stowd` (watch mode)

Runs an initial stow, then restows after any create/write/remove/rename under the repo, debounced.

Flags:

| Flag | Default | Meaning |
|---|---|---|
| `--src` | `$STOWD_DOTFILES_FOLDER_PATH` or `~/.dotfiles` | Dotfiles repo path |
| `--target` | `$HOME` | Target dir to symlink into |
| `--override` | `true` | Pass `--adopt` to stow (pull home-side file content into repo when a conflict exists) |
| `--verbose` | `true` | Verbose stow output |
| `--dry-run` | `false` | Simulate without changes |
| `--timeout` | `30s` | Timeout for stow ops |
| `--debounce` | `800ms` | Debounce window |
| `--exclude` | `.git`, `.DS_Store` | Comma-separated packages to skip |

Default excludes are `.git` and `.DS_Store`; passing `--exclude` replaces them entirely.

### `stowd dotfiles track <package> <path>`

Move a real path from the target (`$HOME`) into a new Stow package, then restow:

```bash
stowd dotfiles track agents ~/.agents
stowd dotfiles track opencode ~/.config/opencode
stowd dotfiles track zsh ~/.zshrc
```

Creates `<src>/<package>/<path-relative-to-home>` plus `.stowd-state/<package>.json` metadata, and a stow-managed symlink back at the original path.

### `stowd dotfiles untrack <package>`

Reverse of `track`: unstows, moves the repo copy back to its home path, removes the package dir and `.stowd-state/<package>.json`:

```bash
stowd dotfiles untrack agents
```

## Failure modes (from stowd source)

| Situation | Error / behavior |
|---|---|
| Package already exists | `package "<name>" already exists` — pick a different name |
| Package name starts with `.` | Rejected — package names cannot start with `.` |
| Package name contains `/` | Rejected — no path separators in package names |
| Source path already a symlink | "track the real source instead" |
| Source path outside target | Must live under the target dir |
| `track` on the whole target | "refusing to track the entire target directory" |
| `untrack` without metadata | Package must have `.stowd-state/<pkg>.json` from a prior `track` |

## Workflows

### Sync a new config (repo-first — auto)
1. Place the file in the repo: `dotfiles/<pkg>/<rel-path>`.
2. Let the watcher stow it (check: `stowd` running, log tail).
3. Confirm the symlink appeared at the original path.

### Adopt an existing home config (home-first — explicit)
1. `stowd dotfiles track <package> <path>` (add `--dry-run` first to preview the move).
2. Verify the repo now holds the file and the symlink points back.
3. `git -C <src> status` to review before committing the repo.

### Untrack a package
1. `stowd dotfiles untrack <package>` (`--dry-run` first).
2. Confirm the file is restored to its home path and the package dir is gone.

## macOS service (launchd)

Built into the repo: `launchd/com.orshemtov.stowd.plist` + `scripts/install-user.sh`.

- **Install / restart with changes**:
  ```bash
  DOTFILES_DIR="$HOME/Projects/dotfiles" TARGET_DIR="$HOME" make install-user
  ```
  Builds `~/.local/bin/stowd`, rewrites the plist, and `bootout`/`bootstrap`/`kickstart -k` to apply.
- **Status**:
  ```bash
  launchctl print gui/$(id -u)/com.orshemtov.stowd
  ```
- **Logs**:
  ```bash
  tail -f ~/Library/Logs/stowd.out.log ~/Library/Logs/stowd.err.log
  ```
- **Uninstall**:
  ```bash
  make uninstall-user
  ```
- The plist uses `RunAtLoad` + `KeepAlive` (except successful exit) and a minimal `PATH` that includes `~/.local/bin`, so GNU Stow resolves. Reload config changes by re-running `make install-user` (kickstart) — there is no separate live-reload.

## Autonomy rules

- **Always `--dry-run` first** for `track` and `untrack` — they physically move files in/out of the repo.
- Inspect-only actions run freely: `launchctl print`, `tail` logs, `stowd` watch output, `git status` in the repo, listing the repo package tree.
- If the user only says "my .config isn't syncing", start by explaining the one-directional model and checking whether the watcher is even running (`launchctl print` / logs) before suggesting `track`.

## Example session

```
$ stowd dotfiles track tuicr ~/.config/tuicr
Running: stow -t /Users/or -Rv tuicr
Ran: stow [-t /Users/or -Rv tuicr]
Tracked /Users/or/.config/tuicr in package tuicr
Dotfiles path: /Users/or/Projects/dotfiles/tuicr/.config/tuicr
```

Then in the dotfiles repo: `git status` shows the new package, ready to commit.
