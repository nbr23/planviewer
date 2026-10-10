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
	"strings"
	"sync"
	"time"
)

type Origin struct {
	Created time.Time
	Project string
}

type transcriptFile struct {
	info   fs.FileInfo
	offset int64
}

type TranscriptIndex struct {
	root     string
	interval time.Duration
	mu       sync.Mutex
	last     time.Time
	files    map[string]*transcriptFile
	origins  map[string]Origin
}

type transcriptRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Cwd       string    `json:"cwd"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Input struct {
		FilePath string `json:"file_path"`
	} `json:"input"`
}

var (
	planPathMarker = []byte("/plans/")
	writeMarker    = []byte(`"Write"`)
)

func NewTranscriptIndex(root string, interval time.Duration) *TranscriptIndex {
	return &TranscriptIndex{
		root:     root,
		interval: interval,
		files:    map[string]*transcriptFile{},
		origins:  map[string]Origin{},
	}
}

func (t *TranscriptIndex) Lookup(name string) (Origin, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	o, ok := t.origins[name]
	return o, ok
}

func (t *TranscriptIndex) Refresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.last.IsZero() && time.Since(t.last) < t.interval {
		return
	}
	t.last = time.Now()
	_ = filepath.WalkDir(t.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		t.scanFile(p, info)
		return nil
	})
}

func (t *TranscriptIndex) scanFile(p string, info fs.FileInfo) {
	tf, ok := t.files[p]
	if !ok || !os.SameFile(tf.info, info) || info.Size() < tf.offset {
		tf = &transcriptFile{}
		t.files[p] = tf
	}
	tf.info = info
	if info.Size() == tf.offset {
		return
	}

	f, err := os.Open(p)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(tf.offset, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		tf.offset += int64(len(line))
		t.indexLine(line)
	}
}

func (t *TranscriptIndex) indexLine(line []byte) {
	if !bytes.Contains(line, planPathMarker) || !bytes.Contains(line, writeMarker) {
		return
	}
	var rec transcriptRecord
	if json.Unmarshal(line, &rec) != nil || rec.Timestamp.IsZero() {
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
