package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

//go:embed web
var webFS embed.FS

const contentSecurityPolicy = "default-src 'self'; img-src 'self' data: https:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

type Server struct {
	store       *Store
	transcripts bool
	md          goldmark.Markdown
	planTmpl    *template.Template
	indexHTML   []byte
}

type listResponse struct {
	Transcripts bool   `json:"transcripts"`
	Total       int    `json:"total"`
	Plans       []Plan `json:"plans"`
}

type planPage struct {
	Plan
	HTML template.HTML
	Raw  string
}

func NewServer(store *Store, transcripts bool) (*Server, error) {
	indexHTML, err := webFS.ReadFile("web/index.html")
	if err != nil {
		return nil, err
	}
	planTmpl, err := template.ParseFS(webFS, "web/plan.html")
	if err != nil {
		return nil, err
	}
	return &Server{
		store:       store,
		transcripts: transcripts,
		md: goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		),
		planTmpl:  planTmpl,
		indexHTML: indexHTML,
	}, nil
}

func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(webFS, "web/static")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/plans", s.handleList)
	mux.HandleFunc("GET /p/{name}", s.handlePlan)
	mux.HandleFunc("GET /raw/{name}", s.handleRaw)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(s.indexHTML)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	plans, err := s.store.List()
	if err != nil {
		log.Printf("list plans: %v", err)
		http.Error(w, "cannot read plans directory", http.StatusInternalServerError)
		return
	}
	total := len(plans)
	plans = filterPlans(plans, parseQuery(r.URL.Query().Get("q")))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(listResponse{Transcripts: s.transcripts, Total: total, Plans: plans})
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	p, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var buf bytes.Buffer
	if err := s.md.Convert(p.body, &buf); err != nil {
		log.Printf("render %s: %v", p.Name, err)
		http.Error(w, "cannot render plan", http.StatusInternalServerError)
		return
	}
	page := planPage{Plan: p, HTML: template.HTML(buf.String()), Raw: string(p.body)}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.planTmpl.Execute(w, page); err != nil {
		log.Printf("template %s: %v", p.Name, err)
	}
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	p, ok := s.lookup(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(p.body)
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (Plan, bool) {
	p, err := s.store.Get(r.PathValue("name"))
	if errors.Is(err, errNotFound) {
		http.NotFound(w, r)
		return Plan{}, false
	}
	if err != nil {
		log.Printf("read plan: %v", err)
		http.Error(w, "cannot read plan", http.StatusInternalServerError)
		return Plan{}, false
	}
	return p, true
}
