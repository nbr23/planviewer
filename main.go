package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const usage = `usage: planviewer [tui] [flags]
       planviewer serve [flags]

Run "planviewer <command> -h" for the flags of a command.
`

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func defaultClaudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

func defaultCodexDir() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".codex"
	}
	return filepath.Join(home, ".codex")
}

func claudeDirFlag(fs *flag.FlagSet) *string {
	dir := new(string)
	def := defaultClaudeDir()
	fs.StringVar(dir, "claude-dir", def, "Claude Code config directory holding plans/ and projects/ with session transcripts")
	fs.StringVar(dir, "d", def, "shorthand for -claude-dir")
	return dir
}

func codexDirFlag(fs *flag.FlagSet) *string {
	return fs.String("codex-dir", defaultCodexDir(), "Codex home directory holding sessions/; empty disables Codex")
}

func cacheDirFlag(fs *flag.FlagSet) *string {
	def := ""
	if d, err := os.UserCacheDir(); err == nil {
		def = filepath.Join(d, "planviewer")
	}
	return fs.String("cache-dir", def, "directory for the transcript index cache; empty disables caching")
}

func withCache(index *TranscriptIndex, cacheDir string) *TranscriptIndex {
	if cacheDir != "" {
		index.EnableCache(filepath.Join(cacheDir, cacheFileName(index.root)))
	}
	return index
}

func claudePaths(dir string) (plans, projects string) {
	return filepath.Join(dir, "plans"), filepath.Join(dir, "projects")
}

func openCatalog(claudeDir, codexDir, cacheDir string) (*Catalog, error) {
	plansDir, projectsDir := claudePaths(claudeDir)
	var store *Store
	var claude, codex *TranscriptIndex
	if isDir(projectsDir) {
		claude = withCache(NewTranscriptIndex(projectsDir, 15*time.Second), cacheDir)
	}
	if isDir(plansDir) {
		store = NewStore(plansDir, claude)
	}
	if sessionsDir := filepath.Join(codexDir, "sessions"); codexDir != "" && isDir(sessionsDir) {
		codex = withCache(NewCodexIndex(sessionsDir, 15*time.Second), cacheDir)
	}
	if store == nil && claude == nil && codex == nil {
		return nil, fmt.Errorf("no plans or sessions found in %q or %q", claudeDir, codexDir)
	}
	return NewCatalog(store, claude, codex), nil
}

func parseCommand(args []string) (string, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "tui", args, nil
	}
	switch args[0] {
	case "tui", "serve":
		return args[0], args[1:], nil
	}
	return "", nil, fmt.Errorf("unknown command %q", args[0])
}

func main() {
	cmd, args, err := parseCommand(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "planviewer: %v\n%s", err, usage)
		os.Exit(2)
	}
	switch cmd {
	case "serve":
		runServe(args)
	case "tui":
		if err := runTUI(args); err != nil {
			fmt.Fprintf(os.Stderr, "planviewer: %v\n", err)
			os.Exit(1)
		}
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dir := claudeDirFlag(fs)
	codexDir := codexDirFlag(fs)
	cacheDir := cacheDirFlag(fs)
	addr := fs.String("addr", envOr("ADDR", ":8080"), "listen address")
	fs.Parse(args)

	catalog, err := openCatalog(*dir, *codexDir, *cacheDir)
	if err != nil {
		log.Fatal(err)
	}
	if catalog.store == nil {
		log.Printf("no Claude plans directory in %q", *dir)
	} else if catalog.store.origins == nil {
		log.Printf("no Claude transcripts directory in %q; created dates fall back to modification time", *dir)
	}
	for _, index := range catalog.indexes {
		go func() {
			start := time.Now()
			index.Refresh()
			log.Printf("indexed transcripts in %s in %s", index.root, time.Since(start).Round(time.Millisecond))
			if err := index.CacheErr(); err != nil {
				log.Printf("transcript cache: %v", err)
			}
		}()
	}

	srv, err := NewServer(catalog)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s on %s", strings.Join(catalog.Sources(), " and "), *addr)
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(httpSrv.ListenAndServe())
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	dir := claudeDirFlag(fs)
	codexDir := codexDirFlag(fs)
	cacheDir := cacheDirFlag(fs)
	fs.Parse(args)

	catalog, err := openCatalog(*dir, *codexDir, *cacheDir)
	if err != nil {
		return err
	}
	return runProgram(catalog)
}
