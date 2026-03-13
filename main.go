package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const dotfilesPathEnvVar = "STOWD_DOTFILES_FOLDER_PATH"

type config struct {
	src      string
	target   string
	override bool
	verbose  bool
	dryRun   bool
	timeout  time.Duration
	debounce time.Duration
	exclude  map[string]struct{}
}

type trackState struct {
	PackageName string `json:"package_name"`
	RelPath     string `json:"rel_path"`
}

func (cfg config) print() {
	fmt.Printf("wstow - Watch and Stow dotfiles\n")
	fmt.Printf("Src: %s\n", cfg.src)
	fmt.Printf("Target: %s\n", cfg.target)
	fmt.Printf("Override: %v\n", cfg.override)
	fmt.Printf("Verbose: %v\n", cfg.verbose)
	fmt.Printf("Dry Run: %v\n", cfg.dryRun)
	fmt.Printf("Timeout: %v\n", cfg.timeout)
	fmt.Printf("Debounce: %v\n", cfg.debounce)
	for ex := range cfg.exclude {
		fmt.Printf("Excluding package: %s\n", ex)
	}
}

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "dotfiles" {
		err = runDotfilesCommand(os.Args[2:])
	} else {
		err = runWatchCommand(os.Args[1:])
	}
	if err != nil {
		log.Fatal(err)
	}
}

func runWatchCommand(args []string) error {
	var cfg config
	fs := flag.NewFlagSet("stowd", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fs.StringVar(&cfg.src, "src", "", srcFlagDescription())
	fs.StringVar(&cfg.target, "target", "", "Path to the target directory")
	fs.BoolVar(&cfg.override, "override", true, "Override existing files")
	fs.BoolVar(&cfg.verbose, "verbose", true, "Enable verbose output")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Simulate actions without making changes")
	fs.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "Timeout for stow operations")
	fs.DurationVar(&cfg.debounce, "debounce", 800*time.Millisecond, "Event debounce window")

	cfg.exclude = map[string]struct{}{
		".git":      {},
		".DS_Store": {},
	}
	fs.Func("exclude", "Comma-separated list of packages to exclude", func(s string) error {
		cfg.exclude = make(map[string]struct{})
		for pkg := range strings.SplitSeq(s, ",") {
			pkg = strings.TrimSpace(pkg)
			if pkg != "" {
				cfg.exclude[pkg] = struct{}{}
			}
		}
		return nil
	})

	if err := fs.Parse(args); err != nil {
		return err
	}

	var err error
	cfg.src, err = resolveSrcDir(cfg.src)
	if err != nil {
		return err
	}
	cfg.target, err = resolveTargetDir(cfg.target)
	if err != nil {
		return err
	}

	if _, err := exec.LookPath("stow"); err != nil {
		return fmt.Errorf("GNU Stow is not installed or not found in PATH: %w", err)
	}

	return watchAndStow(cfg)
}

func runDotfilesCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("missing subcommand; try: stowd dotfiles track <package> <path>")
	}

	switch args[0] {
	case "track":
		return runTrackCommand(args[1:])
	case "untrack":
		return runUntrackCommand(args[1:])
	default:
		return fmt.Errorf("unknown dotfiles subcommand %q", args[0])
	}
}

func runTrackCommand(args []string) error {
	var cfg config
	fs := flag.NewFlagSet("stowd dotfiles track", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fs.StringVar(&cfg.src, "src", "", srcFlagDescription())
	fs.StringVar(&cfg.target, "target", "", "Path to the target directory")
	fs.BoolVar(&cfg.verbose, "verbose", true, "Enable verbose output")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Simulate actions without making changes")

	if err := fs.Parse(args); err != nil {
		return err
	}

	trackArgs := fs.Args()
	if len(trackArgs) != 2 {
		return errors.New("usage: stowd dotfiles track <package> <path>")
	}

	packageName := trackArgs[0]
	pathArg := trackArgs[1]
	if err := validatePackageName(packageName); err != nil {
		return err
	}

	var err error
	cfg.src, err = resolveSrcDir(cfg.src)
	if err != nil {
		return err
	}
	cfg.target, err = resolveTargetDir(cfg.target)
	if err != nil {
		return err
	}

	if _, err := exec.LookPath("stow"); err != nil {
		return fmt.Errorf("GNU Stow is not installed or not found in PATH: %w", err)
	}

	return trackPath(cfg, packageName, pathArg)
}

func runUntrackCommand(args []string) error {
	var cfg config
	fs := flag.NewFlagSet("stowd dotfiles untrack", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fs.StringVar(&cfg.src, "src", "", srcFlagDescription())
	fs.StringVar(&cfg.target, "target", "", "Path to the target directory")
	fs.BoolVar(&cfg.verbose, "verbose", true, "Enable verbose output")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Simulate actions without making changes")

	if err := fs.Parse(args); err != nil {
		return err
	}

	untrackArgs := fs.Args()
	if len(untrackArgs) != 1 {
		return errors.New("usage: stowd dotfiles untrack <package>")
	}

	packageName := untrackArgs[0]
	if err := validatePackageName(packageName); err != nil {
		return err
	}

	var err error
	cfg.src, err = resolveSrcDir(cfg.src)
	if err != nil {
		return err
	}
	cfg.target, err = resolveTargetDir(cfg.target)
	if err != nil {
		return err
	}

	if _, err := exec.LookPath("stow"); err != nil {
		return fmt.Errorf("GNU Stow is not installed or not found in PATH: %w", err)
	}

	return untrackPackage(cfg, packageName)
}

func watchAndStow(cfg config) error {
	cfg.print()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigc
		cancel()
	}()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer watcher.Close()

	var mu sync.Mutex
	added := map[string]bool{}
	src := cfg.src

	var watchDir func(string) error
	watchDir = func(dir string) error {
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil
		}

		rel, _ := filepath.Rel(cfg.src, dir)
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(os.PathSeparator)) {
			return nil
		}

		mu.Lock()
		if added[dir] {
			mu.Unlock()
			return nil
		}
		added[dir] = true
		mu.Unlock()

		if err := watcher.Add(dir); err != nil {
			return err
		}

		entities, err := os.ReadDir(dir)
		if err != nil {
			return err
		}

		for _, e := range entities {
			childPath := filepath.Join(dir, e.Name())
			childInfo, err := os.Lstat(childPath)
			if err != nil {
				return err
			}
			if childInfo.Mode()&os.ModeSymlink != 0 || !childInfo.IsDir() {
				continue
			}
			if err := watchDir(childPath); err != nil {
				return err
			}
		}

		return nil
	}

	if err := watchDir(src); err != nil {
		return fmt.Errorf("failed to add watchers: %w", err)
	}

	log.Printf("Watching %s (target: %s)", cfg.src, cfg.target)

	if err := runStow(cfg); err != nil {
		return fmt.Errorf("initial stow failed: %w", err)
	}

	trigger := make(chan struct{}, 1)
	go func() {
		timer := time.NewTimer(time.Hour)
		_ = timer.Stop()
		for {
			select {
			case <-trigger:
				_ = timer.Stop()
				timer.Reset(cfg.debounce)
			case <-timer.C:
				if err := runStow(cfg); err != nil {
					log.Printf("Restow failed: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case event := <-watcher.Events:
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				if event.Op&fsnotify.Create != 0 {
					info, err := os.Stat(event.Name)
					if err == nil && info.IsDir() {
						_ = watchDir(event.Name)
					}
				}
				select {
				case trigger <- struct{}{}:
				default:
				}
			}
		case err := <-watcher.Errors:
			log.Printf("Watch error: %v\n", err)
		case <-ctx.Done():
			return nil
		}
	}
}

func srcFlagDescription() string {
	return fmt.Sprintf("Path to the source directory, defaults to $%s or ~/.dotfiles", dotfilesPathEnvVar)
}

func getDefaultSrcDir() (string, error) {
	if src := strings.TrimSpace(os.Getenv(dotfilesPathEnvVar)); src != "" {
		return expandAndAbsPath(src)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not get home directory: %w", err)
	}

	return filepath.Join(home, ".dotfiles"), nil
}

func resolveSrcDir(src string) (string, error) {
	if strings.TrimSpace(src) == "" {
		return getDefaultSrcDir()
	}

	return expandAndAbsPath(src)
}

func resolveTargetDir(target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		target = home
	}

	return expandAndAbsPath(target)
}

func expandAndAbsPath(value string) (string, error) {
	expanded, err := expandUserPath(value)
	if err != nil {
		return "", err
	}
	return filepath.Abs(expanded)
}

func expandUserPath(value string) (string, error) {
	value = strings.TrimSpace(os.ExpandEnv(value))
	if value == "" {
		return "", nil
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		if value == "~" {
			return home, nil
		}
		return filepath.Join(home, value[2:]), nil
	}
	return value, nil
}

func validatePackageName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("package name is required")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("package name %q cannot start with '.'", name)
	}
	if strings.Contains(name, string(os.PathSeparator)) {
		return fmt.Errorf("package name %q cannot contain path separators", name)
	}
	return nil
}

func stateDirPath(cfg config) string {
	return filepath.Join(cfg.src, ".stowd-state")
}

func trackStatePath(cfg config, packageName string) string {
	return filepath.Join(stateDirPath(cfg), packageName+".json")
}

func saveTrackState(cfg config, state trackState) error {
	if err := os.MkdirAll(stateDirPath(cfg), 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	return os.WriteFile(trackStatePath(cfg, state.PackageName), payload, 0o644)
}

func loadTrackState(cfg config, packageName string) (trackState, error) {
	statePath := trackStatePath(cfg, packageName)
	payload, err := os.ReadFile(statePath)
	if err != nil {
		return trackState{}, err
	}

	var state trackState
	if err := json.Unmarshal(payload, &state); err != nil {
		return trackState{}, err
	}
	if state.PackageName == "" {
		state.PackageName = packageName
	}
	return state, nil
}

func trackPath(cfg config, packageName, pathArg string) error {
	sourcePath, err := expandAndAbsPath(pathArg)
	if err != nil {
		return fmt.Errorf("failed to resolve %q: %w", pathArg, err)
	}

	if sourcePath == cfg.target {
		return errors.New("refusing to track the entire target directory")
	}
	if isWithinRoot(cfg.src, sourcePath) {
		return fmt.Errorf("%s is already inside the dotfiles repo", sourcePath)
	}

	info, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", sourcePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is already a symlink; track the real source instead", sourcePath)
	}

	if _, err := os.Stat(cfg.src); err != nil {
		if os.IsNotExist(err) {
			if cfg.dryRun {
				log.Printf("Dry run: would create dotfiles repo root %s", cfg.src)
			} else if err := os.MkdirAll(cfg.src, 0o755); err != nil {
				return fmt.Errorf("failed to create dotfiles repo root %s: %w", cfg.src, err)
			}
		} else {
			return fmt.Errorf("failed to access dotfiles repo root %s: %w", cfg.src, err)
		}
	}

	packageDir := filepath.Join(cfg.src, packageName)
	if _, err := os.Stat(packageDir); err == nil {
		return fmt.Errorf("package %q already exists at %s", packageName, packageDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect package path %s: %w", packageDir, err)
	}

	relToTarget, err := filepath.Rel(cfg.target, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to compute path relative to target %s: %w", cfg.target, err)
	}
	if relToTarget == "." || relToTarget == "" || relToTarget == ".." || strings.HasPrefix(relToTarget, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%s must live under target directory %s", sourcePath, cfg.target)
	}

	destinationPath := filepath.Join(packageDir, relToTarget)
	if cfg.dryRun {
		log.Printf("Dry run: would move %s -> %s", sourcePath, destinationPath)
		log.Printf("Dry run: would write track state %s", trackStatePath(cfg, packageName))
		log.Printf("Dry run: would run stow for package %s", packageName)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return fmt.Errorf("failed to prepare destination %s: %w", filepath.Dir(destinationPath), err)
	}

	if err := movePath(sourcePath, destinationPath); err != nil {
		return fmt.Errorf("failed to move %s into dotfiles repo: %w", sourcePath, err)
	}
	if err := saveTrackState(cfg, trackState{PackageName: packageName, RelPath: relToTarget}); err != nil {
		if rollbackErr := rollbackTrackedPath(destinationPath, sourcePath, packageDir); rollbackErr != nil {
			return fmt.Errorf("failed to save track state: %v (rollback also failed: %v)", err, rollbackErr)
		}
		return fmt.Errorf("failed to save track state: %w", err)
	}

	stowCfg := cfg
	stowCfg.override = false
	if err := runStow(stowCfg, packageName); err != nil {
		_ = os.Remove(trackStatePath(cfg, packageName))
		if rollbackErr := rollbackTrackedPath(destinationPath, sourcePath, packageDir); rollbackErr != nil {
			return fmt.Errorf("stow failed: %v (rollback also failed: %v)", err, rollbackErr)
		}
		return fmt.Errorf("stow failed after moving files: %w", err)
	}

	log.Printf("Tracked %s in package %s", sourcePath, packageName)
	log.Printf("Dotfiles path: %s", destinationPath)
	return nil
}

func untrackPackage(cfg config, packageName string) error {
	packageDir := filepath.Join(cfg.src, packageName)
	if _, err := os.Stat(packageDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("package %q does not exist at %s", packageName, packageDir)
		}
		return fmt.Errorf("failed to inspect package path %s: %w", packageDir, err)
	}

	state, err := loadTrackState(cfg, packageName)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("package %q has no stowd track metadata at %s", packageName, trackStatePath(cfg, packageName))
		}
		return fmt.Errorf("failed to read track metadata for package %q: %w", packageName, err)
	}

	if state.RelPath == "" {
		return fmt.Errorf("package %q has invalid track metadata: missing rel_path", packageName)
	}

	repoPath := filepath.Join(packageDir, state.RelPath)
	targetPath := filepath.Join(cfg.target, state.RelPath)
	if _, err := os.Stat(repoPath); err != nil {
		return fmt.Errorf("tracked repo path %s is missing: %w", repoPath, err)
	}

	if cfg.dryRun {
		log.Printf("Dry run: would unstow package %s", packageName)
		log.Printf("Dry run: would move %s -> %s", repoPath, targetPath)
		log.Printf("Dry run: would remove %s and %s", packageDir, trackStatePath(cfg, packageName))
		return nil
	}

	if err := runUnstow(cfg, packageName); err != nil {
		return fmt.Errorf("failed to unstow package %q: %w", packageName, err)
	}

	if err := prepareDestinationForMove(targetPath); err != nil {
		_ = runStow(cfg, packageName)
		return fmt.Errorf("failed to prepare destination %s: %w", targetPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		_ = runStow(cfg, packageName)
		return fmt.Errorf("failed to prepare parent directory %s: %w", filepath.Dir(targetPath), err)
	}
	if err := movePath(repoPath, targetPath); err != nil {
		_ = runStow(cfg, packageName)
		return fmt.Errorf("failed to restore tracked path to %s: %w", targetPath, err)
	}

	cleanupEmptyDirs(filepath.Dir(repoPath), packageDir)
	cleanupEmptyDirs(packageDir, packageDir)
	_ = os.Remove(trackStatePath(cfg, packageName))
	cleanupEmptyDirs(stateDirPath(cfg), stateDirPath(cfg))

	log.Printf("Untracked package %s", packageName)
	log.Printf("Restored path: %s", targetPath)
	return nil
}

func isWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	copyCmd := exec.Command("cp", "-a", src, dst)
	copyCmd.Stdout = os.Stdout
	copyCmd.Stderr = os.Stderr
	if err := copyCmd.Run(); err != nil {
		return err
	}

	return os.RemoveAll(src)
}

func rollbackTrackedPath(destinationPath, sourcePath, packageDir string) error {
	if err := movePath(destinationPath, sourcePath); err != nil {
		return err
	}

	cleanupEmptyDirs(filepath.Dir(destinationPath), packageDir)
	cleanupEmptyDirs(packageDir, packageDir)
	return nil
}

func prepareDestinationForMove(dst string) error {
	info, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(dst)
	}
	if info.IsDir() {
		entries, err := os.ReadDir(dst)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return os.Remove(dst)
		}
		return fmt.Errorf("destination %s already exists and is not empty", dst)
	}

	return fmt.Errorf("destination %s already exists", dst)
}

func cleanupEmptyDirs(start, stop string) {
	current := start
	for {
		if current == "" || current == "." {
			return
		}

		entries, err := os.ReadDir(current)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(current); err != nil {
			return
		}
		if current == stop {
			return
		}
		parent := filepath.Dir(current)
		if parent == current {
			return
		}
		current = parent
	}
}

func listPackages(cfg config) ([]string, error) {
	entries, err := os.ReadDir(cfg.src)
	if err != nil {
		return nil, err
	}

	var packages []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		base := e.Name()
		if _, skip := cfg.exclude[base]; skip {
			continue
		}

		if strings.HasPrefix(base, ".") {
			continue
		}

		packages = append(packages, base)
	}

	return packages, nil
}

func runStow(cfg config, requestedPackages ...string) error {
	packages := requestedPackages
	if len(packages) == 0 {
		var err error
		packages, err = listPackages(cfg)
		if err != nil {
			return err
		}

		if len(packages) == 0 {
			log.Println("No packages to stow")
			return nil
		}
	}

	args := stowArgs(cfg, packages)
	cmd := exec.Command("stow", args...)
	cmd.Dir = cfg.src
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if cfg.dryRun {
		log.Printf("Dry run: stow %s", strings.Join(args, " "))
	} else {
		log.Printf("Running: stow %s", strings.Join(args, " "))
		if err := cmd.Run(); err != nil {
			log.Printf("stow failed")
			return err
		}
	}

	log.Printf("Ran: stow %v", args)
	return nil
}

func runUnstow(cfg config, packages ...string) error {
	if len(packages) == 0 {
		return errors.New("at least one package is required for unstow")
	}

	args := []string{"-t", cfg.target}
	if cfg.verbose {
		if cfg.dryRun {
			args = append(args, "-nDv")
		} else {
			args = append(args, "-Dv")
		}
	} else {
		if cfg.dryRun {
			args = append(args, "-nD")
		} else {
			args = append(args, "-D")
		}
	}
	args = append(args, packages...)

	cmd := exec.Command("stow", args...)
	cmd.Dir = cfg.src
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if cfg.dryRun {
		log.Printf("Dry run: stow %s", strings.Join(args, " "))
	} else {
		log.Printf("Running: stow %s", strings.Join(args, " "))
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	log.Printf("Ran: stow %v", args)
	return nil
}

func stowArgs(cfg config, packages []string) []string {
	args := []string{"-t", cfg.target}
	if cfg.verbose {
		if cfg.dryRun {
			args = append(args, "-nRv")
		} else {
			args = append(args, "-Rv")
		}
	} else {
		if cfg.dryRun {
			args = append(args, "-nR")
		} else {
			args = append(args, "-R")
		}
	}

	if cfg.override {
		args = append(args, "--adopt")
	}

	args = append(args, packages...)
	return args
}
