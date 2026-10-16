package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Origin struct {
	Created time.Time
	Project string
}

type transcriptFile struct {
	mtime   time.Time
	offset  int64
	tail    []byte
	session *sessionBuilder
}

type TranscriptIndex struct {
	root      string
	interval  time.Duration
	mu        sync.Mutex
	last      time.Time
	files     map[string]*transcriptFile
	origins   map[string]Origin
	sessions  map[string]*sessionBuilder
	dirty     bool
	build     string
	cachePath string
	cacheErr  error
}

type transcriptRecord struct {
	Type             string    `json:"type"`
	Timestamp        time.Time `json:"timestamp"`
	Cwd              string    `json:"cwd"`
	GitBranch        string    `json:"gitBranch"`
	IsSidechain      bool      `json:"isSidechain"`
	IsMeta           bool      `json:"isMeta"`
	IsCompactSummary bool      `json:"isCompactSummary"`
	AITitle          string    `json:"aiTitle"`
	CustomTitle      string    `json:"customTitle"`
	Message          struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Text  string `json:"text"`
	Input struct {
		FilePath string `json:"file_path"`
	} `json:"input"`
}

const tailLen = 64

var (
	planPathMarker    = []byte("/plans/")
	writeMarker       = []byte(`"Write"`)
	userMarker        = []byte(`"type":"user"`)
	toolResultMarker  = []byte(`"tool_use_id"`)
	textMarker        = []byte(`"type":"text"`)
	aiTitleMarker     = []byte(`"type":"ai-title"`)
	customTitleMarker = []byte(`"type":"custom-title"`)
)

func NewTranscriptIndex(root string, interval time.Duration) *TranscriptIndex {
	return &TranscriptIndex{
		root:     filepath.Clean(root),
		interval: interval,
		files:    map[string]*transcriptFile{},
		origins:  map[string]Origin{},
		sessions: map[string]*sessionBuilder{},
	}
}

func (t *TranscriptIndex) Lookup(name string) (Origin, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	o, ok := t.origins[name]
	return o, ok
}

func (t *TranscriptIndex) Sessions() []Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Session, 0, len(t.sessions))
	for _, b := range t.sessions {
		if len(b.turns) > 0 {
			out = append(out, b.snapshot(false))
		}
	}
	slices.SortFunc(out, func(a, b Session) int { return b.Updated.Compare(a.Updated) })
	return out
}

func (t *TranscriptIndex) Session(id string) (Session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, ok := t.sessions[id]
	if !ok || len(b.turns) == 0 {
		return Session{}, false
	}
	return b.snapshot(true), true
}

func (t *TranscriptIndex) Refresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.last.IsZero() && time.Since(t.last) < t.interval {
		return
	}
	t.last = time.Now()
	seen := make(map[string]bool, len(t.files))
	_ = filepath.WalkDir(t.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		seen[p] = true
		t.scanFile(p, info)
		return nil
	})
	for p, tf := range t.files {
		if seen[p] {
			continue
		}
		delete(t.files, p)
		t.dirty = true
		if tf.session != nil && t.sessions[tf.session.id] == tf.session {
			delete(t.sessions, tf.session.id)
		}
	}
	if t.dirty && t.cachePath != "" {
		t.saveCache()
	}
}

func (t *TranscriptIndex) newSession(p string) *sessionBuilder {
	if filepath.Dir(filepath.Dir(p)) != t.root {
		return nil
	}
	b := &sessionBuilder{id: strings.TrimSuffix(filepath.Base(p), ".jsonl")}
	t.sessions[b.id] = b
	return b
}

func (t *TranscriptIndex) scanFile(p string, info fs.FileInfo) {
	tf, ok := t.files[p]
	if ok && info.Size() == tf.offset && info.ModTime().Equal(tf.mtime) {
		return
	}

	f, err := os.Open(p)
	if err != nil {
		return
	}
	defer f.Close()
	if !ok || info.Size() < tf.offset || !bytes.Equal(readTail(f, tf.offset), tf.tail) {
		tf = &transcriptFile{session: t.newSession(p)}
		t.files[p] = tf
	}
	tf.mtime = info.ModTime()
	t.dirty = true
	if _, err := f.Seek(tf.offset, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break
		}
		tf.offset += int64(len(line))
		t.indexLine(line, tf.session)
	}
	tf.tail = readTail(f, tf.offset)
}

func readTail(f *os.File, offset int64) []byte {
	buf := make([]byte, min(offset, tailLen))
	if _, err := f.ReadAt(buf, offset-int64(len(buf))); err != nil {
		return nil
	}
	return buf
}

func (t *TranscriptIndex) indexLine(line []byte, session *sessionBuilder) {
	origin := bytes.Contains(line, planPathMarker) && bytes.Contains(line, writeMarker)
	if !origin && (session == nil || !sessionLine(line)) {
		return
	}
	var rec transcriptRecord
	if json.Unmarshal(line, &rec) != nil {
		return
	}
	if origin {
		t.indexOrigin(&rec)
	}
	if session != nil {
		session.add(&rec)
	}
}

func sessionLine(line []byte) bool {
	if bytes.Contains(line, userMarker) {
		return !bytes.Contains(line, toolResultMarker)
	}
	return bytes.Contains(line, textMarker) || bytes.Contains(line, aiTitleMarker) || bytes.Contains(line, customTitleMarker)
}

func (t *TranscriptIndex) indexOrigin(rec *transcriptRecord) {
	if rec.Timestamp.IsZero() {
		return
	}
	var blocks []contentBlock
	if json.Unmarshal(rec.Message.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		if b.Type != "tool_use" || b.Name != "Write" || path.Base(path.Dir(b.Input.FilePath)) != "plans" {
			continue
		}
		name := path.Base(b.Input.FilePath)
		if cur, ok := t.origins[name]; ok && !rec.Timestamp.Before(cur.Created) {
			continue
		}
		t.origins[name] = Origin{Created: rec.Timestamp, Project: projectName(rec.Cwd)}
	}
}

func projectName(cwd string) string {
	if cwd == "" {
		return ""
	}
	return path.Base(cwd)
}
