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
	"strconv"
	"strings"
	"sync"
	"time"
)

type Origin struct {
	Created time.Time
	Project string
	Session string
}

type transcriptFile struct {
	mtime   time.Time
	offset  int64
	tail    []byte
	session *sessionBuilder
}

type transcriptFormat interface {
	source() string
	files(root string) []string
	open(t *TranscriptIndex, p string) *sessionBuilder
	indexLine(t *TranscriptIndex, line []byte, tf *transcriptFile)
}

type sessionMeta interface {
	refresh()
	annotate(s *Session) bool
}

type claudeFormat struct{}

type TranscriptIndex struct {
	root      string
	format    transcriptFormat
	meta      sessionMeta
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
	SessionID        string    `json:"sessionId"`
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
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Text      string `json:"text"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
	Input     struct {
		FilePath     string `json:"file_path"`
		PlanFilePath string `json:"planFilePath"`
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
	exitPlanMarker    = []byte(`"ExitPlanMode"`)
	toolUseMarker     = []byte(`"type":"tool_use"`)
)

func NewTranscriptIndex(root string, interval time.Duration) *TranscriptIndex {
	return newTranscriptIndex(root, claudeFormat{}, interval)
}

func newTranscriptIndex(root string, format transcriptFormat, interval time.Duration) *TranscriptIndex {
	return &TranscriptIndex{
		root:     filepath.Clean(root),
		format:   format,
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

func (t *TranscriptIndex) Status(key string) planStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	var latest planReview
	for _, b := range t.sessions {
		if r, ok := b.review(key); ok && (latest.status == "" || r.at.After(latest.at)) {
			latest = r
		}
	}
	return latest.status
}

func (t *TranscriptIndex) Sessions() []Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Session, 0, len(t.sessions))
	hidden := t.hidden()
	for _, b := range t.sessions {
		if len(b.turns) == 0 || hidden[b.id] {
			continue
		}
		if s, ok := t.snapshot(b, false); ok {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b Session) int { return b.Updated.Compare(a.Updated) })
	return out
}

func (t *TranscriptIndex) Session(id string) (Session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, ok := t.sessions[id]
	if !ok || len(b.turns) == 0 || t.hidden()[id] {
		return Session{}, false
	}
	return t.snapshot(b, true)
}

func (t *TranscriptIndex) snapshot(b *sessionBuilder, withTurns bool) (Session, bool) {
	s := b.snapshot(withTurns)
	s.Source = t.format.source()
	if t.meta != nil && !t.meta.annotate(&s) {
		return Session{}, false
	}
	return s, true
}

func (t *TranscriptIndex) hidden() map[string]bool {
	out := map[string]bool{}
	for _, b := range t.sessions {
		for _, id := range b.children {
			out[id] = true
		}
	}
	return out
}

func (t *TranscriptIndex) cwd(id string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Session{ID: id}
	if b, ok := t.sessions[id]; ok {
		s.Cwd = b.cwd
	}
	if t.meta != nil {
		t.meta.annotate(&s)
	}
	return s.Cwd
}

func (t *TranscriptIndex) Plans() []Plan {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Plan
	for _, b := range t.sessions {
		for i := range b.plans {
			out = append(out, t.plan(b, i))
		}
	}
	return out
}

func (t *TranscriptIndex) Plan(name string) (Plan, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range t.sessions {
		for i := range b.plans {
			if planName(b.id, i) == name {
				return t.plan(b, i), true
			}
		}
	}
	return Plan{}, false
}

func (t *TranscriptIndex) plan(b *sessionBuilder, i int) Plan {
	sp := b.plans[i]
	name := planName(b.id, i)
	r, _ := b.review(name)
	return Plan{
		Name:         name,
		Source:       t.format.source(),
		Title:        extractTitle(name, []byte(sp.text)),
		Created:      sp.at,
		CreatedExact: true,
		Updated:      sp.at,
		Project:      projectName(b.cwd),
		Session:      b.id,
		Size:         int64(len(sp.text)),
		Status:       r.status,
		body:         []byte(sp.text),
	}
}

func planName(sessionID string, i int) string {
	if i == 0 {
		return sessionID
	}
	return sessionID + "-" + strconv.Itoa(i+1)
}

func (t *TranscriptIndex) Refresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.last.IsZero() && time.Since(t.last) < t.interval {
		return
	}
	t.last = time.Now()
	seen := make(map[string]bool, len(t.files))
	for _, p := range t.format.files(t.root) {
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		seen[p] = true
		t.scanFile(p, info)
	}
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
	if t.meta != nil {
		t.meta.refresh()
	}
	if t.dirty && t.cachePath != "" {
		t.saveCache()
	}
}

func (t *TranscriptIndex) addSession(id string) *sessionBuilder {
	b := &sessionBuilder{id: id}
	t.sessions[id] = b
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
		tf = &transcriptFile{session: t.format.open(t, p)}
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
		t.format.indexLine(t, line, tf)
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

func (claudeFormat) source() string { return sourceClaude }

func (claudeFormat) files(root string) []string { return jsonlFiles(root) }

func jsonlFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && strings.HasSuffix(p, ".jsonl") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func (claudeFormat) open(t *TranscriptIndex, p string) *sessionBuilder {
	if filepath.Dir(filepath.Dir(p)) != t.root {
		return nil
	}
	return t.addSession(strings.TrimSuffix(filepath.Base(p), ".jsonl"))
}

func (claudeFormat) indexLine(t *TranscriptIndex, line []byte, tf *transcriptFile) {
	b := tf.session
	origin := bytes.Contains(line, planPathMarker) && bytes.Contains(line, writeMarker)
	session := b != nil && sessionLine(line)
	review := b != nil && claudeReviewLine(b, line)
	if !origin && !session && !review {
		return
	}
	var rec transcriptRecord
	if json.Unmarshal(line, &rec) != nil {
		return
	}
	if origin {
		session := rec.SessionID
		if b != nil {
			session = b.id
		}
		if name := t.indexOrigin(&rec, session); name != "" && b != nil && !rec.IsSidechain {
			b.planFile = name
		}
	}
	if session {
		b.add(&rec)
	}
	if review {
		addClaudeReview(b, &rec)
	}
}

func claudeReviewLine(b *sessionBuilder, line []byte) bool {
	return bytes.Contains(line, exitPlanMarker) ||
		bytes.Contains(line, toolResultMarker) && b.awaitingResult(line) ||
		bytes.Contains(line, toolUseMarker) && b.awaitingWork()
}

func addClaudeReview(b *sessionBuilder, rec *transcriptRecord) {
	if rec.IsSidechain {
		return
	}
	var blocks []contentBlock
	if json.Unmarshal(rec.Message.Content, &blocks) != nil {
		return
	}
	for _, c := range blocks {
		switch {
		case rec.Type == "user" && c.Type == "tool_result" && c.ToolUseID != "":
			b.resolve(c.ToolUseID, !c.IsError, rec.Timestamp)
		case rec.Type != "assistant" || c.Type != "tool_use":
			continue
		case c.Name == "ExitPlanMode":
			key := b.planFile
			if c.Input.PlanFilePath != "" {
				key = path.Base(c.Input.PlanFilePath)
			}
			if key != "" {
				b.present(key, c.ID, rec.Timestamp)
			}
		case c.Name != "EnterPlanMode":
			b.work(rec.Timestamp)
		}
	}
}

func sessionLine(line []byte) bool {
	if bytes.Contains(line, userMarker) {
		return !bytes.Contains(line, toolResultMarker)
	}
	return bytes.Contains(line, textMarker) || bytes.Contains(line, aiTitleMarker) || bytes.Contains(line, customTitleMarker)
}

func (t *TranscriptIndex) indexOrigin(rec *transcriptRecord, session string) (name string) {
	var blocks []contentBlock
	if json.Unmarshal(rec.Message.Content, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type != "tool_use" || b.Name != "Write" || path.Base(path.Dir(b.Input.FilePath)) != "plans" {
			continue
		}
		name = path.Base(b.Input.FilePath)
		if cur, ok := t.origins[name]; rec.Timestamp.IsZero() || ok && !rec.Timestamp.Before(cur.Created) {
			continue
		}
		t.origins[name] = Origin{Created: rec.Timestamp, Project: projectName(rec.Cwd), Session: session}
	}
	return name
}

func projectName(cwd string) string {
	if cwd == "" {
		return ""
	}
	return path.Base(cwd)
}
