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
	"os"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

//go:embed web
var webFS embed.FS

const contentSecurityPolicy = "default-src 'self'; img-src 'self' data: https:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

type Server struct {
	catalog *Catalog
	md      goldmark.Markdown
	tmpl    *template.Template
}

type listResponse struct {
	Transcripts bool     `json:"transcripts"`
	Sources     []string `json:"sources"`
	Total       int      `json:"total"`
	Plans       []Plan   `json:"plans"`
}

type sessionListResponse struct {
	Sources  []string  `json:"sources"`
	Total    int       `json:"total"`
	Sessions []Session `json:"sessions"`
}

type indexPage struct {
	Kind        string
	Title       string
	Placeholder string
	CountKey    string
	CountLabel  string
	Transcripts bool
}

type planPage struct {
	Plan
	HTML        template.HTML
	Raw         string
	ShowSource  bool
	SessionLink bool
}

type turnView struct {
	Who  string
	At   time.Time
	HTML template.HTML
}

type sessionPage struct {
	Session
	Turns      []turnView
	Resume     string
	ShowSource bool
}

var (
	plansIndex    = indexPage{Kind: "plans", Title: "Plans", Placeholder: listKinds[plansMode].placeholder, CountKey: "size", CountLabel: "Size"}
	sessionsIndex = indexPage{Kind: "sessions", Title: "Sessions", Placeholder: listKinds[sessionsMode].placeholder, CountKey: "messages", CountLabel: "Messages"}
)

func NewServer(catalog *Catalog) (*Server, error) {
	tmpl, err := template.ParseFS(webFS, "web/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{
		catalog: catalog,
		md: goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		),
		tmpl: tmpl,
	}, nil
}

func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(webFS, "web/static")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/plans", s.handleList)
	mux.HandleFunc("GET /p/{name...}", s.handlePlan)
	mux.HandleFunc("GET /raw/{name...}", s.handleRaw)
	if s.catalog.HasSessions() {
		mux.HandleFunc("GET /sessions", s.handleSessionsIndex)
		mux.HandleFunc("GET /api/sessions", s.handleSessions)
		mux.HandleFunc("GET /s/{id}", s.handleSession)
	}
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
	page := plansIndex
	page.Transcripts = s.catalog.HasSessions()
	s.execute(w, "index.html", page)
}

func (s *Server) handleSessionsIndex(w http.ResponseWriter, r *http.Request) {
	page := sessionsIndex
	page.Transcripts = true
	s.execute(w, "index.html", page)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	plans, err := s.catalog.Plans()
	if err != nil {
		log.Printf("list plans: %v", err)
		http.Error(w, "cannot read plans directory", http.StatusInternalServerError)
		return
	}
	total := len(plans)
	plans = filterPlans(plans, parseQuery(r.URL.Query().Get("q")))
	writeJSON(w, listResponse{Transcripts: s.catalog.HasSessions(), Sources: s.catalog.Sources(), Total: total, Plans: plans})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.catalog.Sessions()
	total := len(sessions)
	sessions = filterSessions(sessions, parseQuery(r.URL.Query().Get("q")))
	writeJSON(w, sessionListResponse{Sources: s.catalog.Sources(), Total: total, Sessions: sessions})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	p, ok := s.lookup(w, r)
	if !ok {
		return
	}
	html, err := s.render(p.body)
	if err != nil {
		log.Printf("render %s: %v", p.Name, err)
		http.Error(w, "cannot render plan", http.StatusInternalServerError)
		return
	}
	_, linked := s.catalog.Session(p.Session)
	s.execute(w, "plan.html", planPage{Plan: p, HTML: html, Raw: string(p.body), ShowSource: s.showSource(), SessionLink: linked})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.catalog.Session(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	home, _ := os.UserHomeDir()
	page := sessionPage{Session: sess, Resume: sess.resumeCommand(home), ShowSource: s.showSource()}
	for _, t := range sess.turns {
		html, err := s.render([]byte(t.text))
		if err != nil {
			log.Printf("render session %s: %v", sess.ID, err)
			http.Error(w, "cannot render session", http.StatusInternalServerError)
			return
		}
		who := sess.agentName()
		if t.user {
			who = "You"
		}
		page.Turns = append(page.Turns, turnView{Who: who, At: t.at, HTML: html})
	}
	s.execute(w, "session.html", page)
}

func (s *Server) showSource() bool {
	return len(s.catalog.Sources()) > 1
}

func (s *Server) render(md []byte) (template.HTML, error) {
	var buf bytes.Buffer
	if err := s.md.Convert(md, &buf); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func (s *Server) execute(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template %s: %v", name, err)
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
	p, err := s.catalog.Plan(r.PathValue("name"))
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
