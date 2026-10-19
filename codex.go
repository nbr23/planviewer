package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	codexMetaMarker = []byte(`"type":"session_meta"`)
	codexMarkers    = [][]byte{
		[]byte(`"type":"user_message"`),
		[]byte(`"type":"agent_message"`),
		[]byte(`"type":"UserMessage"`),
		[]byte(`"type":"AgentMessage"`),
		[]byte(`"type":"Plan"`),
	}
)

type codexFormat struct{}

type codexRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Payload   struct {
		Type   string          `json:"type"`
		ID     string          `json:"id"`
		Cwd    string          `json:"cwd"`
		Source json.RawMessage `json:"source"`
		Git    struct {
			Branch string `json:"branch"`
		} `json:"git"`
		Message string `json:"message"`
		Item    struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"item"`
	} `json:"payload"`
}

type codexTitles struct {
	path  string
	mtime time.Time
	size  int64
	names map[string]string
}

func NewCodexIndex(sessionsDir string, interval time.Duration) *TranscriptIndex {
	t := newTranscriptIndex(sessionsDir, codexFormat{}, interval)
	t.meta = &codexTitles{path: filepath.Join(filepath.Dir(t.root), "session_index.jsonl")}
	return t
}

func (codexFormat) source() string { return sourceCodex }

func (codexFormat) files(root string) []string { return jsonlFiles(root) }

func (codexFormat) open(*TranscriptIndex, string) *sessionBuilder {
	return nil
}

func (codexFormat) indexLine(t *TranscriptIndex, line []byte, tf *transcriptFile) {
	if tf.session == nil {
		// Forked subagent rollouts repeat the parent's session_meta further down; only the first line describes the file.
		if tf.offset == int64(len(line)) && bytes.Contains(line, codexMetaMarker) {
			startCodexSession(t, line, tf)
		}
		return
	}
	if !containsAny(line, codexMarkers) {
		return
	}
	var rec codexRecord
	if json.Unmarshal(line, &rec) == nil && rec.Type == "event_msg" {
		addCodexEvent(tf.session, &rec)
	}
}

func startCodexSession(t *TranscriptIndex, line []byte, tf *transcriptFile) {
	var rec codexRecord
	if json.Unmarshal(line, &rec) != nil || rec.Type != "session_meta" || rec.Payload.ID == "" || codexSubagent(rec.Payload.Source) {
		return
	}
	tf.session = t.addSession(rec.Payload.ID)
	tf.session.cwd, tf.session.branch = rec.Payload.Cwd, rec.Payload.Git.Branch
}

func containsAny(line []byte, markers [][]byte) bool {
	for _, m := range markers {
		if bytes.Contains(line, m) {
			return true
		}
	}
	return false
}

func codexSubagent(source json.RawMessage) bool {
	var s struct {
		Subagent json.RawMessage `json:"subagent"`
	}
	return json.Unmarshal(source, &s) == nil && s.Subagent != nil
}

func addCodexEvent(b *sessionBuilder, rec *codexRecord) {
	p := &rec.Payload
	var user bool
	var text string
	switch p.Type {
	case "user_message":
		user, text = true, p.Message
	case "agent_message":
		text = p.Message
	case "item_completed":
		switch p.Item.Type {
		case "UserMessage":
			user, text = true, codexItemText(rec)
		case "AgentMessage":
			text = codexItemText(rec)
		case "Plan":
			text = strings.TrimSpace(p.Item.Text)
			if text != "" {
				b.plans = append(b.plans, sessionPlan{at: rec.Timestamp, text: text})
			}
		}
	}
	if text = strings.TrimSpace(text); text != "" {
		b.addTurn(user, rec.Timestamp, text)
	}
}

func codexItemText(rec *codexRecord) string {
	var parts []string
	for _, c := range rec.Payload.Item.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (c *codexTitles) annotate(s *Session) bool {
	if name := c.names[s.ID]; name != "" {
		s.Title = name
	}
	return true
}

func (c *codexTitles) refresh() {
	info, err := os.Stat(c.path)
	if err != nil {
		c.names, c.mtime, c.size = nil, time.Time{}, 0
		return
	}
	if info.ModTime().Equal(c.mtime) && info.Size() == c.size {
		return
	}
	f, err := os.Open(c.path)
	if err != nil {
		return
	}
	defer f.Close()
	names := map[string]string{}
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		var entry struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.ID != "" {
			if name := strings.TrimSpace(entry.ThreadName); name != "" {
				names[entry.ID] = name
			}
		}
		if err != nil {
			break
		}
	}
	c.names, c.mtime, c.size = names, info.ModTime(), info.Size()
}
