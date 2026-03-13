# stowd

A file watcher that automatically runs [`stow`](https://www.gnu.org/software/stow/) whenever a file is created, deleted, or renamed — keeping your dotfiles in sync.

---

## Install

Requires **Go ≥ 1.24**

```bash
git clone https://github.com/orshemtov/stowd.git
cd stowd
go build
```

This will produce a binary called `stowd`.

---

## Usage

```bash
# Uses $STOWD_DOTFILES_FOLDER_PATH if set, otherwise ~/.dotfiles
./stowd

# Override the dotfiles repo path explicitly
./stowd --src ~/Projects/dotfiles
```

Set a custom default dotfiles path with `STOWD_DOTFILES_FOLDER_PATH`, for example in `~/.zshenv`:

```bash
export STOWD_DOTFILES_FOLDER_PATH="$HOME/Projects/dotfiles"
```

### Flags

| Flag      | Type                 | Description                |
|------------|----------------------|----------------------------|
| `--src`      | string             | Path to the dotfiles repo (defaults to `$STOWD_DOTFILES_FOLDER_PATH` or `~/.dotfiles`) |
| `--target`   | string             | Target directory           |
| `--override` | bool               | Override existing files    |
| `--verbose`  | bool               | Enable verbose output      |
| `--dry-run`  | bool               | Run without making changes |
| `--timeout`  | time.Duration      | Operation timeout          |
| `--debounce` | time.Duration      | Debounce interval (e.g. `2s`) |
| `--exclude`  | map[string]struct{} | Patterns to exclude      |

### Track an existing file or directory

Use `track` to move a real path from `$HOME` into a new Stow package and then restow it:

```bash
stowd dotfiles track agents ~/.agents
stowd dotfiles track opencode ~/.config/opencode
stowd dotfiles track zsh ~/.zshrc
```

This creates:

- `<dotfiles>/<package>/<path-relative-to-home>` in the repo
- a stow-managed symlink back at the original path

Rules:

- package name is required
- tracked path must live under the target directory (defaults to `$HOME`)
- tracking fails if the package already exists
- use `--src`, `--target`, or `--dry-run` with `track` as needed

### Untrack a managed package

Use `untrack` to move a path that was previously tracked by `stowd` back out of the dotfiles repo into the target directory:

```bash
stowd dotfiles untrack agents
```

Rules:

- untracking requires the package name
- the package must have been originally created by `stowd dotfiles track`
- `untrack` restores the tracked path back into the target directory and removes the package from the repo

---

## Running `stowd` as a macOS service

You can run `stowd` continuously in the background using **launchd**, the macOS equivalent of `systemd`.

### 1. Install the service

```bash
# clone the repo if you haven’t already
git clone https://github.com/orshemtov/stowd.git
cd stowd

# build and install as a user service (runs after login)
DOTFILES_DIR="${STOWD_DOTFILES_FOLDER_PATH:-$HOME/.dotfiles}" TARGET_DIR="$HOME" make install-user
```

This will:

- Build the binary into `~/.local/bin/stowd`
- Create a `LaunchAgent` plist at `~/Library/LaunchAgents/com.orshemtov.stowd.plist`
- Start and enable the service automatically at login
- Log output to `~/Library/Logs/stowd.out.log` and `stowd.err.log`

### 2. Check status and logs

```bash
launchctl print gui/$(id -u)/com.orshemtov.stowd
tail -f ~/Library/Logs/stowd.out.log ~/Library/Logs/stowd.err.log
```

### 3. Uninstall the service

```bash
make uninstall-user
```

---

## Directory structure

```
stowd/
├─ launchd/
│  └─ com.orshemtov.stowd.plist      # launchd service definition
├─ scripts/
│  ├─ install-user.sh                # install and start user service
│  └─ uninstall-user.sh              # stop and remove service
├─ Makefile                          # build/install helpers
└─ main.go                           # main source
```

---

## Example logs

```
2025/10/25 11:23:44 Watching /Users/or/Projects/dotfiles
2025/10/25 11:23:46 Detected change: .zshrc modified
2025/10/25 11:23:46 Running: stow --target=/Users/or --dir=/Users/or/Projects/dotfiles
2025/10/25 11:23:47 ✅ stow completed successfully
```

---

## Replicating on another Mac

1. Clone the repo  
2. Run:

   ```bash
    DOTFILES_DIR="${STOWD_DOTFILES_FOLDER_PATH:-$HOME/.dotfiles}" TARGET_DIR="$HOME" make install-user
   ```

That’s it — `stowd` will now watch your dotfiles automatically after every login.

---

## License

MIT © Or Shemtov
