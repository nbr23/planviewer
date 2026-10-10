package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"
)

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

func main() {
	plansDir := flag.String("plans", envOr("PLANS_DIR", "/plans"), "directory containing plan markdown files")
	projectsDir := flag.String("projects", envOr("PROJECTS_DIR", "/projects"), "optional Claude Code projects directory with session transcripts")
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	flag.Parse()

	if !isDir(*plansDir) {
		log.Fatalf("plans directory %q not found", *plansDir)
	}

	var origins *TranscriptIndex
	if isDir(*projectsDir) {
		origins = NewTranscriptIndex(*projectsDir, 15*time.Second)
		go func() {
			start := time.Now()
			origins.Refresh()
			log.Printf("indexed transcripts in %s in %s", *projectsDir, time.Since(start).Round(time.Millisecond))
		}()
	} else {
		log.Printf("no transcripts directory at %q; created dates fall back to modification time", *projectsDir)
	}

	srv, err := NewServer(NewStore(*plansDir, origins), origins != nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s on %s", *plansDir, *addr)
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(httpSrv.ListenAndServe())
}
