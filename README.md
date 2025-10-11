# stowctl

A file watcher that runs `stow` every time a file is created, deleted or renamed.

## Install

Requires `go`

```bash
git clone https://github.com/orshemtov/stowctl.git
go build
```

## Usage

```bash
# replace ~/Projects/dotfiles with the path to your dotfiles repo
./stowctl --repo ~/Projects/dotfiles
```

### Flags

| Flag      | Type              | Description                |
|-----------|-------------------|----------------------------|
| repo      | string            | Path to the dotfiles repo  |
| target    | string            | Target directory           |
| override  | bool              | Override existing files    |
| verbose   | bool              | Enable verbose output      |
| dryRun    | bool              | Run without making changes |
| timeout   | time.Duration     | Operation timeout          |
| debounce  | time.Duration     | Debounce interval          |
| exclude   | map[string]struct{} | Patterns to exclude      |
