package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	errNotFound  = errors.New("plan not found")
	validNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.md$`)
	headingMarks = "# \t"
)

type Plan struct {
	Name         string    `json:"name"`
	Title        string    `json:"title"`
	Created      time.Time `json:"created"`
	CreatedExact bool      `json:"createdExact"`
	Updated      time.Time `json:"updated"`
	Project      string    `json:"project,omitempty"`
	Size         int64     `json:"size"`
	Snippet      string    `json:"snippet,omitempty"`
	body         []byte
}

type cachedFile struct {
	mtime time.Time
	size  int64
	title string
	body  []byte
}

type Store struct {
	dir     string
	origins *TranscriptIndex
	mu      sync.Mutex
	cache   map[string]*cachedFile
}

func NewStore(dir string, origins *TranscriptIndex) *Store {
	return &Store{dir: dir, origins: origins, cache: map[string]*cachedFile{}}
}

func validName(name string) bool {
	return validNameRe.MatchString(name)
}

func extractTitle(name string, body []byte) string {
	for _, line := range bytes.Split(body, []byte("\n")) {
		t := strings.TrimSpace(string(line))
		if t == "" {
			continue
		}
		if t = strings.TrimLeft(t, headingMarks); t != "" {
			return t
		}
		break
	}
	return strings.TrimSuffix(name, ".md")
}

func (s *Store) List() ([]Plan, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	if s.origins != nil {
		s.origins.Refresh()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]bool, len(entries))
	plans := make([]Plan, 0, len(entries))
	for _, e := range entries {
		if !e.Type().IsRegular() || !validName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cf, err := s.load(e.Name(), info)
		if err != nil {
			continue
		}
		seen[e.Name()] = true
		plans = append(plans, s.plan(e.Name(), cf))
	}
	for name := range s.cache {
		if !seen[name] {
			delete(s.cache, name)
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].Updated.After(plans[j].Updated) })
	return plans, nil
}

func (s *Store) Get(name string) (Plan, error) {
	if !validName(name) {
		return Plan{}, errNotFound
	}
	info, err := os.Lstat(filepath.Join(s.dir, name))
	if err != nil || !info.Mode().IsRegular() {
		return Plan{}, errNotFound
	}
	if s.origins != nil {
		s.origins.Refresh()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cf, err := s.load(name, info)
	if err != nil {
		return Plan{}, err
	}
	return s.plan(name, cf), nil
}

func (s *Store) load(name string, info fs.FileInfo) (*cachedFile, error) {
	if cf, ok := s.cache[name]; ok && cf.mtime.Equal(info.ModTime()) && cf.size == info.Size() {
		return cf, nil
	}
	body, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return nil, err
	}
	cf := &cachedFile{
		mtime: info.ModTime(),
		size:  info.Size(),
		title: extractTitle(name, body),
		body:  body,
	}
	s.cache[name] = cf
	return cf, nil
}

func (s *Store) plan(name string, cf *cachedFile) Plan {
	p := Plan{
		Name:    name,
		Title:   cf.title,
		Created: cf.mtime.UTC(),
		Updated: cf.mtime.UTC(),
		Size:    cf.size,
		body:    cf.body,
	}
	if s.origins == nil {
		return p
	}
	if o, ok := s.origins.Lookup(name); ok {
		p.Created = o.Created
		p.CreatedExact = true
		p.Project = o.Project
	}
	return p
}
