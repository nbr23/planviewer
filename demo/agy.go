package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"path/filepath"
	"time"
)

type agyStep struct {
	StepIndex int       `json:"step_index"`
	Source    string    `json:"source"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Content   string    `json:"content,omitempty"`
	ToolCalls []any     `json:"tool_calls,omitempty"`
}

func (d demo) agyDir() string { return filepath.Join(d.geminiDir(), "antigravity-cli") }

func (d demo) writeAgy() error {
	convs := map[string]any{}
	for _, s := range agySessions {
		if err := d.writeAgySession(s); err != nil {
			return err
		}
		convs[s.id] = map[string]any{"is_internal": false, "summary": map[string]any{
			"Title":         s.title,
			"Preview":       s.events[0].text,
			"WorkspaceURIs": []string{(&url.URL{Scheme: "file", Path: d.cwd(s.project)}).String()},
		}}
	}
	raw, err := json.Marshal(map[string]any{"conversations": convs})
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(d.agyDir(), "cache", "conversation_metadata.json"), raw)
}

func (d demo) writeAgySession(s session) error {
	brain := filepath.Join(d.agyDir(), "brain", s.id)
	recorded := filepath.Join(d.home, ".gemini", "antigravity-cli", "brain", s.id)
	var buf bytes.Buffer
	steps := 0
	step := func(source, typ string, at time.Time, content string, calls ...any) {
		appendJSON(&buf, agyStep{steps, source, typ, at, content, calls})
		steps++
	}
	var presented string
	for _, e := range s.events {
		at := d.at(e.ago)
		switch e.kind {
		case promptEvent, rejectEvent:
			step("USER_EXPLICIT", "USER_INPUT", at, "<USER_REQUEST>\n"+e.text+"\n</USER_REQUEST>")
		case approveEvent:
			step("USER_EXPLICIT", "USER_INPUT", at, "[Approved] "+presented)
		case replyEvent:
			step("MODEL", "PLANNER_RESPONSE", at, e.text)
		case planEvent, draftEvent:
			text := body(e.text)
			step("MODEL", "PLANNER_RESPONSE", at, "", map[string]any{"name": "write_to_file", "args": map[string]any{
				"TargetFile":       filepath.Join(recorded, e.file),
				"CodeContent":      text,
				"ArtifactMetadata": map[string]any{"RequestFeedback": e.kind == planEvent},
			}})
			if e.kind == planEvent {
				presented = e.file
			}
			if err := writeArtifact(brain, e.file, text, at); err != nil {
				return err
			}
		case workEvent:
			step("MODEL", "PLANNER_RESPONSE", at, "", map[string]any{"name": "run_command", "args": map[string]any{"CommandLine": e.text}})
		}
	}
	return writeFile(filepath.Join(brain, ".system_generated", "logs", "transcript.jsonl"), buf.Bytes())
}

func writeArtifact(brain, file, text string, at time.Time) error {
	meta, err := json.Marshal(map[string]any{"updatedAt": at})
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(brain, file+".metadata.json"), meta); err != nil {
		return err
	}
	return writeStamped(filepath.Join(brain, file), []byte(text), at)
}
