package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

var projectDirRe = regexp.MustCompile(`[^A-Za-z0-9]`)

func (d demo) writeClaude() error {
	for _, s := range claudeSessions {
		if err := d.writeClaudeSession(s); err != nil {
			return err
		}
	}
	for _, e := range claudeLoosePlans {
		if err := d.writeClaudePlan(e); err != nil {
			return err
		}
	}
	return nil
}

func (d demo) writeClaudePlan(e event) error {
	return writeStamped(filepath.Join(d.claudeDir(), "plans", e.file), []byte(body(e.text)), d.at(e.ago))
}

func (d demo) writeClaudeSession(s session) error {
	cwd := d.cwd(s.project)
	var buf bytes.Buffer
	record := func(typ string, at time.Time, content any) {
		appendJSON(&buf, map[string]any{
			"type": typ, "timestamp": at, "sessionId": s.id, "cwd": cwd, "gitBranch": s.branch,
			"message": map[string]any{"role": typ, "content": content},
		})
	}
	tools := 0
	toolUse := func(name string, input map[string]any) (string, []any) {
		tools++
		id := fmt.Sprintf("toolu_01%.8s%04d", s.id, tools)
		return id, []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}
	}
	if s.title != "" {
		appendJSON(&buf, map[string]any{"type": "ai-title", "aiTitle": s.title, "sessionId": s.id})
	}
	var exitPlanID string
	for _, e := range s.events {
		at := d.at(e.ago)
		switch e.kind {
		case promptEvent:
			record("user", at, e.text)
		case replyEvent:
			record("assistant", at, []any{map[string]any{"type": "text", "text": e.text}})
		case planEvent, draftEvent:
			planPath := filepath.Join(d.home, ".claude", "plans", e.file)
			_, write := toolUse("Write", map[string]any{"file_path": planPath, "content": body(e.text)})
			record("assistant", at, write)
			if err := d.writeClaudePlan(e); err != nil {
				return err
			}
			if e.kind == planEvent {
				var exit []any
				exitPlanID, exit = toolUse("ExitPlanMode", map[string]any{"plan": body(e.text), "planFilePath": planPath})
				record("assistant", at.Add(2*time.Second), exit)
			}
		case approveEvent:
			record("user", at, []any{toolResult(exitPlanID, false, "User has approved your plan. You can now start coding.")})
		case rejectEvent:
			record("user", at, []any{toolResult(exitPlanID, true, "The user doesn't want to proceed with this tool use. The tool use was rejected.")})
			record("user", at.Add(time.Second), e.text)
		case workEvent:
			_, run := toolUse("Bash", map[string]any{"command": e.text})
			record("assistant", at, run)
		}
	}
	return writeFile(filepath.Join(d.claudeDir(), "projects", projectDirRe.ReplaceAllString(cwd, "-"), s.id+".jsonl"), buf.Bytes())
}

func toolResult(id string, isError bool, text string) map[string]any {
	return map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": isError, "content": text}
}
