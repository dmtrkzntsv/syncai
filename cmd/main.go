package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syncai/internal/model"
	"syncai/internal/version"
	"syscall"
	"time"

	"syncai/internal/config"
	"syncai/internal/selfupdate"
	"syncai/internal/syncai"
	"syncai/internal/util"
)

func main() {
	var cfgPath string
	var doSelfUpdate bool
	var showVersion bool
	var noWatch bool
	var workingDir string
	var help bool
	flag.StringVar(&cfgPath, "config", "syncai.json", "path to configuration file")
	flag.StringVar(&workingDir, "workdir", "", "base working directory for relative paths (overrides config)")
	flag.BoolVar(&doSelfUpdate, "self-update", false, "update SyncAI to the latest released version")
	flag.BoolVar(&noWatch, "no-watch", false, "run only the initial sync")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&help, "help", false, "show available commands and their descriptions")
	flag.Parse()

	if help {
		fmt.Println("SyncAI - a lightweight utility that keeps AI-assistant guidelines, rules and ignored files in sync across multiple agents:\n")
		fmt.Println("GitHub: https://github.com/flowmitry/syncai/")
		fmt.Println("Version: ", version.Version(), "\n")
		fmt.Println("Available commands:")
		fmt.Println("  -config string")
		fmt.Println("        path to configuration file (default \"syncai.json\")")
		fmt.Println("  -workdir string")
		fmt.Println("        base working directory for relative paths (overrides config)")
		fmt.Println("  -self-update")
		fmt.Println("        update SyncAI to the latest released version")
		fmt.Println("  -no-watch")
		fmt.Println("        run only the initial sync")
		fmt.Println("  -version")
		fmt.Println("        print version and exit")
		fmt.Println("  -help")
		fmt.Println("        show available commands and their descriptions")
		return
	}

	if showVersion {
		fmt.Println(version.Version())
		return
	}

	if doSelfUpdate {
		if err := selfupdate.Run(); err != nil {
			log.Fatalf("self-update failed: %v", err)
		}
		fmt.Println("SyncAI updated successfully. Restart if it was running.")
		return
	}

	cfg, err := config.Load(cfgPath, workingDir)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	fmt.Printf("SyncAI %s\nGitHub: https://github.com/flowmitry/syncai/\n\n", version.Version())
	fmt.Println("Config path: <", cfgPath, ">")
	err = os.Chdir(cfg.WorkingDir())
	if err != nil {
		log.Fatalf("failed to chdir to %q: %v", cfg.WorkingDir(), err)
	}
	fmt.Println("Base path: <", cfg.WorkingDir(), ">")

	sync := syncai.New(cfg)
	initialSync(cfg, sync)

	if noWatch {
		fmt.Println("SyncAI completed the initial sync")
		return
	}

	fmt.Println("Start watching for file changes...")
	filesState := buildFilesState(cfg)
	scan := func() {
		newState := make(map[string]string)
		for _, agent := range cfg.Agents {
			for _, path := range agent.Files() {
				_, err := os.Stat(path)
				if err != nil {
					if os.IsNotExist(err) {
						// File may not exist yet; silently ignore
						continue
					}
					log.Printf("file error %s: %v", path, err)
					continue
				}
				hash, _ := util.FileHash(path)
				newState[path] = hash
				prev, ok := filesState[path]
				filesState[path] = hash
				if !ok || hash != prev {
					if ok {
						log.Printf("Detected change in %s (%s), syncing...", path, agent.Name)
					} else {
						log.Printf("Detected new file %s (%s), syncing...", path, agent.Name)
					}
					updatedFiles, err := sync.Sync(path)
					if err != nil {
						log.Printf("sync error: %v", err)
					}
					for _, newPath := range updatedFiles {
						filesState[newPath], _ = util.FileHash(newPath)
					}
				}
			}
		}
		for path := range filesState {
			if _, ok := newState[path]; !ok {
				deletedPaths, err := sync.Delete(path)
				for _, deletedPath := range deletedPaths {
					log.Printf("Deleted file across agents: %s", deletedPath)
					delete(filesState, deletedPath)
				}
				if err != nil {
					log.Printf("delete error: %v", err)
				}
			}
		}
	}

	ticker := time.NewTicker(cfg.Interval())
	defer ticker.Stop()

	// Handle OS signals to terminate gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			scan()
		case <-sigCh:
			log.Println("Exiting SyncAI...")
			return
		}
	}
}

// Initial sync: pick the newest version among agents for each logical file (by kind+stem) and propagate it
func initialSync(cfg config.Config, sync *syncai.SyncAI) {
	log.Println("Initial sync started...")
	type newest struct {
		path string
		mod  time.Time
	}
	latest := make(map[string]newest)

	for _, agent := range cfg.Agents {
		for _, path := range agent.Files() {
			fi, err := os.Stat(path)
			if err != nil {
				// File might not exist yet; skip
				continue
			}
			modT := fi.ModTime()

			_, kind, stem, rel := sync.Identify(path)
			if kind == model.KindUnknown {
				continue
			}

			key := string(kind) + "|" + stem + "|" + rel
			if cur, ok := latest[key]; !ok || modT.After(cur.mod) {
				latest[key] = newest{path: path, mod: modT}
			}
		}
	}

	for _, v := range latest {
		if _, err := sync.Sync(v.path); err != nil {
			log.Printf("initial sync error for %s: %v", v.path, err)
		}
	}
	log.Println("Initial sync completed.")
}

func buildFilesState(cfg config.Config) map[string]string {
	hashes := make(map[string]string)
	for _, agent := range cfg.Agents {
		for _, path := range agent.Files() {
			if h, err := util.FileHash(path); err != nil {
				log.Printf("hash error %s: %v", path, err)
			} else {
				hashes[path] = h
			}
		}
	}
	return hashes
}
