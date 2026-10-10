package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeRecord(ts, cwd, filePath string) string {
	rec := map[string]any{
		"type":      "assistant",
		"timestamp": ts,
		"cwd":       cwd,
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "writing the plan"},
				map[string]any{"type": "tool_use", "name": "Write", "input": map[string]any{"file_path": filePath, "content": "# T"}},
			},
		},
	}
	b, _ := json.Marshal(rec)
	return string(b) + "\n"
}

func appendFile(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func TestTranscriptIndex(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "-home-u-dev-app", "sess", "subagents")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(root, "-home-u-dev-app", "sess.jsonl")
	sub := filepath.Join(sessionDir, "agent.jsonl")

	appendFile(t, session, writeRecord("2026-09-02T10:00:00Z", "/home/u/dev/app", "/home/u/.claude/plans/brave-red-fox.md"))
	appendFile(t, session, `{"type":"user","message":{"content":"mention /plans/ and \"Write\" in a string"}}`+"\n")
	appendFile(t, session, writeRecord("2026-09-01T09:00:00Z", "/home/u/dev/other", "/home/u/dev/other/docs/notes.md"))
	appendFile(t, sub, writeRecord("2026-09-01T08:00:00Z", "/home/u/dev/app", "/home/u/.claude/plans/brave-red-fox.md"))

	idx := NewTranscriptIndex(root, 0)
	idx.Refresh()

	o, ok := idx.Lookup("brave-red-fox.md")
	if !ok || !o.Created.Equal(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)) || o.Project != "app" {
		t.Fatalf("Lookup(brave-red-fox.md) = %+v, %v", o, ok)
	}
	if _, ok := idx.Lookup("notes.md"); ok {
		t.Fatal("non-plan Write was indexed")
	}

	partial := writeRecord("2026-08-01T00:00:00Z", "/home/u/dev/late", "/home/u/.claude/plans/late-plan.md")
	appendFile(t, session, partial[:len(partial)/2])
	idx.Refresh()
	if _, ok := idx.Lookup("late-plan.md"); ok {
		t.Fatal("partial line was indexed")
	}

	appendFile(t, session, partial[len(partial)/2:])
	idx.Refresh()
	if o, ok := idx.Lookup("late-plan.md"); !ok || o.Project != "late" {
		t.Fatalf("appended line not picked up: %+v, %v", o, ok)
	}
}

func TestTranscriptIndexRescansReplacedFile(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "s.jsonl")
	appendFile(t, p, writeRecord("2026-09-05T00:00:00Z", "/x/a", "/h/.claude/plans/one.md")+writeRecord("2026-09-05T00:00:01Z", "/x/a", "/h/.claude/plans/pad.md"))

	idx := NewTranscriptIndex(root, 0)
	idx.Refresh()

	if err := os.WriteFile(p, []byte(writeRecord("2026-09-04T00:00:00Z", "/x/b", "/h/.claude/plans/two.md")), 0o600); err != nil {
		t.Fatal(err)
	}
	idx.Refresh()
	if _, ok := idx.Lookup("two.md"); !ok {
		t.Fatal("truncated and rewritten transcript was not rescanned")
	}
	if _, ok := idx.Lookup("one.md"); !ok {
		t.Fatal("previously indexed origin was dropped")
	}
}

func TestTranscriptIndexThrottle(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "s.jsonl")
	idx := NewTranscriptIndex(root, time.Hour)
	idx.Refresh()
	appendFile(t, p, writeRecord("2026-09-05T00:00:00Z", "/x/a", "/h/.claude/plans/one.md"))
	idx.Refresh()
	if _, ok := idx.Lookup("one.md"); ok {
		t.Fatal("refresh was not throttled")
	}
}
