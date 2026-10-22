package main

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"time"
)

type indexCache struct {
	Build   string
	Root    string
	Files   map[string]cachedTranscript
	Origins map[string]Origin
}

type cachedTranscript struct {
	ModTime time.Time
	Offset  int64
	Tail    []byte
	Session *cachedSession
}

type cachedSession struct {
	ID                   string
	Cwd, Branch          string
	CustomTitle, AITitle string
	FirstPrompt          string
	Created, Updated     time.Time
	Turns                []cachedTurn
	Plans                []cachedPlan
	PlanFile             string
	Reviews              []cachedReview
	Children             []string
	Body                 []byte
}

type cachedTurn struct {
	User bool
	At   time.Time
	Text string
}

type cachedPlan struct {
	At   time.Time
	Text string
}

type cachedReview struct {
	Key     string
	Status  planStatus
	At      time.Time
	ToolID  string
	Replied bool
}

func cacheFileName(projectsDir string) string {
	abs, err := filepath.Abs(projectsDir)
	if err != nil {
		abs = projectsDir
	}
	h := fnv.New64a()
	h.Write([]byte(abs))
	return fmt.Sprintf("transcripts-%x.gob", h.Sum64())
}

func (t *TranscriptIndex) EnableCache(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	build, err := buildID()
	if err != nil {
		t.cacheErr = err
		return
	}
	t.build, t.cachePath = build, path
	c, err := readCache(path)
	if err != nil {
		t.cacheErr = err
		return
	}
	if c != nil && c.Build == build && c.Root == t.root {
		t.restore(c)
	}
}

func (t *TranscriptIndex) CacheErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cacheErr
}

func (t *TranscriptIndex) restore(c *indexCache) {
	for p, ct := range c.Files {
		tf := &transcriptFile{mtime: ct.ModTime, offset: ct.Offset, tail: ct.Tail}
		if ct.Session != nil {
			tf.session = ct.Session.builder()
			t.sessions[tf.session.id] = tf.session
		}
		t.files[p] = tf
	}
	maps.Copy(t.origins, c.Origins)
}

func (t *TranscriptIndex) saveCache() {
	c := indexCache{
		Build:   t.build,
		Root:    t.root,
		Files:   make(map[string]cachedTranscript, len(t.files)),
		Origins: t.origins,
	}
	for p, tf := range t.files {
		ct := cachedTranscript{ModTime: tf.mtime, Offset: tf.offset, Tail: tf.tail}
		if tf.session != nil {
			ct.Session = tf.session.cached()
		}
		c.Files[p] = ct
	}
	if err := writeCache(t.cachePath, &c); err != nil {
		t.cacheErr, t.cachePath = err, ""
		return
	}
	t.dirty = false
}

func (b *sessionBuilder) cached() *cachedSession {
	c := &cachedSession{
		ID:          b.id,
		Cwd:         b.cwd,
		Branch:      b.branch,
		CustomTitle: b.customTitle,
		AITitle:     b.aiTitle,
		FirstPrompt: b.firstPrompt,
		Created:     b.created,
		Updated:     b.updated,
		Turns:       make([]cachedTurn, len(b.turns)),
		Plans:       make([]cachedPlan, len(b.plans)),
		PlanFile:    b.planFile,
		Reviews:     make([]cachedReview, len(b.reviews)),
		Children:    b.children,
		Body:        b.body,
	}
	for i, tr := range b.turns {
		c.Turns[i] = cachedTurn{User: tr.user, At: tr.at, Text: tr.text}
	}
	for i, sp := range b.plans {
		c.Plans[i] = cachedPlan{At: sp.at, Text: sp.text}
	}
	for i, r := range b.reviews {
		c.Reviews[i] = cachedReview{Key: r.key, Status: r.status, At: r.at, ToolID: r.toolID, Replied: r.replied}
	}
	return c
}

func (c *cachedSession) builder() *sessionBuilder {
	b := &sessionBuilder{
		id:          c.ID,
		cwd:         c.Cwd,
		branch:      c.Branch,
		customTitle: c.CustomTitle,
		aiTitle:     c.AITitle,
		firstPrompt: c.FirstPrompt,
		created:     c.Created,
		updated:     c.Updated,
		turns:       make([]turn, len(c.Turns)),
		plans:       make([]sessionPlan, len(c.Plans)),
		planFile:    c.PlanFile,
		reviews:     make([]planReview, len(c.Reviews)),
		children:    c.Children,
		body:        c.Body,
	}
	for i, tr := range c.Turns {
		b.turns[i] = turn{user: tr.User, at: tr.At, text: tr.Text}
	}
	for i, sp := range c.Plans {
		b.plans[i] = sessionPlan{at: sp.At, text: sp.Text}
	}
	for i, r := range c.Reviews {
		b.reviews[i] = planReview{key: r.Key, status: r.Status, at: r.At, toolID: r.ToolID, replied: r.Replied}
	}
	return b
}

func readCache(path string) (*indexCache, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var c indexCache
	if gob.NewDecoder(f).Decode(&c) != nil {
		return nil, nil
	}
	return &c, nil
}

func writeCache(path string, c *indexCache) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	err = errors.Join(gob.NewEncoder(f).Encode(c), f.Close())
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

func buildID() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	f, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
