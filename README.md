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
# Replace ~/Projects/dotfiles with the path to your dotfiles repo
./stowd --src ~/Projects/dotfiles
```

### Flags

| Flag      | Type                 | Description                |
|------------|----------------------|----------------------------|
| `--src`      | string             | Path to the dotfiles repo  |
| `--target`   | string             | Target directory           |
| `--override` | bool               | Override existing files    |
| `--verbose`  | bool               | Enable verbose output      |
| `--dryRun`   | bool               | Run without making changes |
| `--timeout`  | time.Duration      | Operation timeout          |
| `--debounce` | time.Duration      | Debounce interval (e.g. `2s`) |
| `--exclude`  | map[string]struct{} | Patterns to exclude      |

---

## Running `stowd` as a macOS service

You can run `stowd` continuously in the background using **launchd**, the macOS equivalent of `systemd`.

### 1. Install the service

```bash
# clone the repo if you haven’t already
git clone https://github.com/orshemtov/stowd.git
cd stowd

# build and install as a user service (runs after login)
DOTFILES_DIR="$HOME/Projects/dotfiles" TARGET_DIR="$HOME" make install-user
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
│  └─ com.orshemtov.stowd.plist     # launchd service definition
├─ scripts/
│  ├─ install-user.sh                # install and start user service
│  └─ uninstall-user.sh              # stop and remove service
├─ Makefile                          # build/install helpers
└─ stowd.go                          # main source
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
   DOTFILES_DIR="$HOME/Projects/dotfiles" TARGET_DIR="$HOME" make install-user
   ```

That’s it — `stowd` will now watch your dotfiles automatically after every login.

---

## License

MIT © Or Shemtov
