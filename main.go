package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

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
	var cfg config

	flag.StringVar(&cfg.src, "src", "", "Path to the source directory, defaults to $HOME/Projects/dotfiles")
	flag.StringVar(&cfg.target, "target", "", "Path to the target directory")
	flag.BoolVar(&cfg.override, "override", true, "Override existing files")
	flag.BoolVar(&cfg.verbose, "verbose", true, "Enable verbose output")
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "Simulate actions without making changes")
	flag.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "Timeout for stow operations")
	flag.DurationVar(&cfg.debounce, "debounce", 800*time.Millisecond, "Event debounce window")

	cfg.exclude = map[string]struct{}{
		".git":      {},
		".DS_Store": {},
	}
	flag.Func("exclude", "Comma-separated list of packages to exclude", func(s string) error {
		cfg.exclude = make(map[string]struct{})
		for pkg := range strings.SplitSeq(s, ",") {
			pkg = strings.TrimSpace(pkg)
			if pkg != "" {
				cfg.exclude[pkg] = struct{}{}
			}
		}
		return nil
	})
	flag.Parse()

	// If no source dir provided, use default
	if cfg.src == "" {
		cfg.src = getDefaultSrcDir()
	}

	// Get the absolute path of the source directory containing the dotfiles
	src, err := filepath.Abs(cfg.src)
	if err != nil {
		log.Fatalf("Failed to get absolute path of src: %v", err)
	}
	cfg.src = src

	// Get the path to the destination where we want to symlink the dotfiles
	if cfg.target == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get user home directory: %v", err)
		}
		cfg.target = home
	}

	if _, err := exec.LookPath("stow"); err != nil {
		log.Fatalf("GNU Stow is not installed or not found in PATH: %v", err)
	}

	// Print configuration
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
		log.Fatalf("Failed to create file watcher: %v", err)
	}
	defer watcher.Close()

	var mu sync.Mutex
	added := map[string]bool{}

	// Walk the source directory and add watchers for all dirs
	// Except .git
	var watchDir func(string) error
	watchDir = func(dir string) error {
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
			if e.IsDir() {
				if err := watchDir(filepath.Join(dir, e.Name())); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if err := watchDir(src); err != nil {
		log.Fatalf("Failed to add watchers: %v", err)
	}

	log.Printf("Watching %s (target: %s)", cfg.src, cfg.target)

	// Initial stow
	if err := runStow(cfg); err != nil {
		log.Fatalf("Initial stow failed: %v", err)
	}

	// Debounced trigger
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
					log.Fatalf("Restow failed: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case event := <-watcher.Events:
			if event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				if event.Op&fsnotify.Create != 0 {
					info, err := os.Stat(event.Name)
					if err == nil && info.IsDir() {
						_ = watchDir(event.Name) // Ignore error
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
			return
		}
	}
}

func getDefaultSrcDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic("could not get home directory")
	}
	return path.Join(home, "Projects", "dotfiles")
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

func runStow(cfg config) error {
	packages, err := listPackages(cfg)
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		log.Println("No packages to stow")
		return nil
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
		err := cmd.Run()
		if err != nil {
			log.Printf("stow failed")
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
