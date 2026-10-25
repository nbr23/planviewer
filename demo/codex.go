package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"time"
)

func (d demo) writeCodex() error {
	var index bytes.Buffer
	for _, s := range codexSessions {
		if err := d.writeCodexSession(s); err != nil {
			return err
		}
		if s.title != "" {
			appendJSON(&index, map[string]any{"id": s.id, "thread_name": s.title, "updated_at": d.at(s.events[len(s.events)-1].ago)})
		}
	}
	return writeFile(filepath.Join(d.codexDir(), "session_index.jsonl"), index.Bytes())
}

func (d demo) writeCodexSession(s session) error {
	start := d.at(s.events[0].ago).Add(-3 * time.Second)
	var buf bytes.Buffer
	line := func(at time.Time, typ string, payload map[string]any) {
		appendJSON(&buf, map[string]any{"timestamp": at, "type": typ, "payload": payload})
	}
	item := func(at time.Time, item map[string]any) {
		line(at, "event_msg", map[string]any{"type": "item_completed", "item": item})
	}
	message := func(at time.Time, typ, text string) {
		item(at, map[string]any{"type": typ, "content": []any{map[string]any{"type": "text", "text": text}}})
	}
	line(start, "session_meta", map[string]any{
		"id": s.id, "timestamp": start, "cwd": d.cwd(s.project), "source": "cli", "git": map[string]any{"branch": s.branch},
	})
	for _, e := range s.events {
		at := d.at(e.ago)
		switch e.kind {
		case promptEvent, rejectEvent:
			message(at, "UserMessage", e.text)
		case replyEvent:
			message(at, "AgentMessage", e.text)
		case planEvent:
			item(at, map[string]any{"type": "Plan", "text": body(e.text)})
		case approveEvent:
			line(at, "turn_context", map[string]any{"collaboration_mode": map[string]any{"mode": "default"}})
			message(at, "UserMessage", "Implement the plan.")
		case workEvent:
			item(at, map[string]any{"type": "CommandExecution", "command": e.text})
		}
	}
	name := fmt.Sprintf("rollout-%s-%s.jsonl", start.Format("2006-01-02T15-04-05"), s.id)
	return writeFile(filepath.Join(d.codexDir(), "sessions", filepath.FromSlash(start.Format("2006/01/02")), name), buf.Bytes())
}
