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

func claudeDirFlag(fs *flag.FlagSet) *string {
	dir := new(string)
	def := defaultClaudeDir()
	fs.StringVar(dir, "claude-dir", def, "Claude Code config directory holding plans/ and, optionally, projects/ with session transcripts")
	fs.StringVar(dir, "d", def, "shorthand for -claude-dir")
	return dir
}

func cacheDirFlag(fs *flag.FlagSet) *string {
	def := ""
	if d, err := os.UserCacheDir(); err == nil {
		def = filepath.Join(d, "planviewer")
	}
	return fs.String("cache-dir", def, "directory for the transcript index cache; empty disables caching")
}

func newIndex(projectsDir, cacheDir string) *TranscriptIndex {
	index := NewTranscriptIndex(projectsDir, 15*time.Second)
	if cacheDir != "" {
		index.EnableCache(filepath.Join(cacheDir, cacheFileName(projectsDir)))
	}
	return index
}

func claudePaths(dir string) (plans, projects string) {
	return filepath.Join(dir, "plans"), filepath.Join(dir, "projects")
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
	cacheDir := cacheDirFlag(fs)
	addr := fs.String("addr", envOr("ADDR", ":8080"), "listen address")
	fs.Parse(args)
	plansDir, projectsDir := claudePaths(*dir)

	if !isDir(plansDir) {
		log.Fatalf("plans directory %q not found", plansDir)
	}

	var index *TranscriptIndex
	if isDir(projectsDir) {
		index = newIndex(projectsDir, *cacheDir)
		go func() {
			start := time.Now()
			index.Refresh()
			log.Printf("indexed transcripts in %s in %s", projectsDir, time.Since(start).Round(time.Millisecond))
			if err := index.CacheErr(); err != nil {
				log.Printf("transcript cache: %v", err)
			}
		}()
	} else {
		log.Printf("no transcripts directory at %q; created dates fall back to modification time and sessions are unavailable", projectsDir)
	}

	srv, err := NewServer(NewStore(plansDir, index), index)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s on %s", plansDir, *addr)
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
	cacheDir := cacheDirFlag(fs)
	fs.Parse(args)
	plansDir, projectsDir := claudePaths(*dir)

	if !isDir(plansDir) {
		return fmt.Errorf("plans directory %q not found", plansDir)
	}
	var index *TranscriptIndex
	if isDir(projectsDir) {
		index = newIndex(projectsDir, *cacheDir)
	}
	return runProgram(NewStore(plansDir, index), index)
}
