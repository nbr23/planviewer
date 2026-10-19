package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func codexLine(ts, typ string, payload map[string]any) string {
	return jsonLine(map[string]any{"timestamp": ts, "type": typ, "payload": payload})
}

func codexMeta(id string, source any) string {
	return codexLine("2026-09-01T10:00:00Z", "session_meta", map[string]any{
		"id": id, "cwd": "/home/u/dev/app", "source": source, "git": map[string]any{"branch": "main"},
	})
}

func codexItem(ts string, item map[string]any) string {
	return codexLine(ts, "event_msg", map[string]any{"type": "item_completed", "item": item})
}

func codexMessage(ts, itemType, text string) string {
	return codexItem(ts, map[string]any{"type": itemType, "content": []any{map[string]any{"type": "text", "text": text}}})
}

func codexPlan(ts, text string) string {
	return codexItem(ts, map[string]any{"type": "Plan", "text": text})
}

func codexFixture(t *testing.T) (home, sessionsDir string) {
	t.Helper()
	home = t.TempDir()
	sessionsDir = filepath.Join(home, "sessions")
	if err := os.MkdirAll(filepath.Join(sessionsDir, "2026", "09", "01"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, sessionsDir
}

func rollout(sessionsDir, id string) string {
	return filepath.Join(sessionsDir, "2026", "09", "01", "rollout-2026-09-01T10-00-00-"+id+".jsonl")
}

func planNames(plans []Plan) []string {
	var out []string
	for _, p := range plans {
		out = append(out, p.Name)
	}
	slices.Sort(out)
	return out
}

func TestCodexIndex(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	home, sessionsDir := codexFixture(t)
	current := rollout(sessionsDir, "new-1")
	appendFile(t, current, strings.Join([]string{
		codexMeta("new-1", "cli"),
		codexMessage("2026-09-01T10:00:01Z", "UserMessage", "plan the rug cleaning"),
		codexLine("2026-09-01T10:00:02Z", "event_msg", map[string]any{"type": "token_count"}),
		codexMessage("2026-09-01T10:00:03Z", "AgentMessage", "Reading the rugs."),
		codexLine("2026-09-01T10:00:04Z", "response_item", map[string]any{"type": "agent_message", "content": []any{map[string]any{"type": "input_text", "text": "inter-agent chatter"}}}),
		codexItem("2026-09-01T10:00:05Z", map[string]any{"type": "CommandExecution", "aggregated_output": "command output"}),
		codexPlan("2026-09-01T10:00:06Z", "# Rug plan\n\nVacuum first."),
		codexMessage("2026-09-01T10:01:00Z", "UserMessage", "Implement the plan."),
		codexMessage("2026-09-01T10:02:00Z", "AgentMessage", "Done."),
	}, ""))
	appendFile(t, rollout(sessionsDir, "old-1"), strings.Join([]string{
		codexMeta("old-1", "cli"),
		codexLine("2026-08-01T10:00:00Z", "event_msg", map[string]any{"type": "user_message", "message": "legacy prompt"}),
		codexLine("2026-08-01T10:00:01Z", "event_msg", map[string]any{"type": "agent_message", "message": "legacy reply"}),
	}, ""))
	appendFile(t, rollout(sessionsDir, "sub-1"), strings.Join([]string{
		codexMeta("sub-1", map[string]any{"subagent": map[string]any{"other": "guardian"}}),
		codexMeta("new-1", "cli"),
		codexMessage("2026-09-01T10:00:02Z", "UserMessage", "subagent prompt"),
		codexPlan("2026-09-01T10:00:03Z", "# Subagent plan"),
	}, ""))
	titles := filepath.Join(home, "session_index.jsonl")
	appendFile(t, titles, jsonLine(map[string]any{"id": "new-1", "thread_name": "Old name"})+jsonLine(map[string]any{"id": "new-1", "thread_name": "Rug cleaning"}))

	idx := NewCodexIndex(sessionsDir, 0)
	idx.Refresh()

	sessions := idx.Sessions()
	if len(sessions) != 2 || sessions[0].ID != "new-1" || sessions[1].ID != "old-1" {
		t.Fatalf("Sessions() = %+v", sessions)
	}
	s := sessions[0]
	if s.Source != sourceCodex || s.Title != "Rug cleaning" || s.Project != "app" || s.Branch != "main" || s.Messages != 4 {
		t.Fatalf("session = %+v", s)
	}
	if got := s.resumeCommand("/home/u"); got != "cd ~/dev/app && codex resume new-1" {
		t.Fatalf("resumeCommand = %q", got)
	}
	want := []string{"plan the rug cleaning", "Reading the rugs.\n\n# Rug plan\n\nVacuum first.", "Implement the plan.", "Done."}
	if got := sessionTexts(t, idx, "new-1"); !slices.Equal(got, want) {
		t.Fatalf("turns = %q, want %q", got, want)
	}
	full, _ := idx.Session("new-1")
	for _, hidden := range []string{"inter-agent", "command output", "subagent"} {
		if bytes.Contains(full.body, []byte(hidden)) {
			t.Errorf("body contains %q", hidden)
		}
	}
	if got := sessionTexts(t, idx, "old-1"); !slices.Equal(got, []string{"legacy prompt", "legacy reply"}) || sessions[1].Title != "legacy prompt" {
		t.Fatalf("legacy turns = %q, title %q", got, sessions[1].Title)
	}

	plans := idx.Plans()
	if got := planNames(plans); !slices.Equal(got, []string{"new-1"}) {
		t.Fatalf("plans = %v", got)
	}
	p := plans[0]
	if p.Source != sourceCodex || p.Title != "Rug plan" || p.Project != "app" || !p.CreatedExact ||
		!p.Created.Equal(time.Date(2026, 9, 1, 10, 0, 6, 0, time.UTC)) || string(p.body) != "# Rug plan\n\nVacuum first." {
		t.Fatalf("plan = %+v", p)
	}

	appendFile(t, current, codexPlan("2026-09-01T10:03:00Z", "# Rug plan, revised"))
	idx.Refresh()
	if got := planNames(idx.Plans()); !slices.Equal(got, []string{"new-1", "new-1-2"}) {
		t.Fatalf("after append plans = %v", got)
	}
	if p, ok := idx.Plan("new-1-2"); !ok || p.Title != "Rug plan, revised" {
		t.Fatalf("Plan(new-1-2) = %+v, %v", p, ok)
	}
	if _, ok := idx.Plan("sub-1"); ok {
		t.Fatal("subagent plan was indexed")
	}

	if err := os.WriteFile(titles, []byte(jsonLine(map[string]any{"id": "new-1", "thread_name": "Persian rug cleaning"})), 0o600); err != nil {
		t.Fatal(err)
	}
	idx.Refresh()
	if s, _ := idx.Session("new-1"); s.Title != "Persian rug cleaning" {
		t.Fatalf("title after rename = %q", s.Title)
	}

	os.Remove(current)
	idx.Refresh()
	if _, ok := idx.Session("new-1"); ok || len(idx.Plans()) != 0 {
		t.Fatal("deleted rollout still indexed")
	}
}

func TestCodexCacheWarmStart(t *testing.T) {
	_, sessionsDir := codexFixture(t)
	appendFile(t, rollout(sessionsDir, "new-1"), codexMeta("new-1", "cli")+
		codexMessage("2026-09-01T10:00:01Z", "UserMessage", "plan it")+
		codexPlan("2026-09-01T10:00:02Z", "# Cached plan"))
	cache := filepath.Join(t.TempDir(), "index.gob")

	cold := NewCodexIndex(sessionsDir, 0)
	cold.EnableCache(cache)
	cold.Refresh()

	warm := NewCodexIndex(sessionsDir, time.Hour)
	warm.EnableCache(cache)
	if err := warm.CacheErr(); err != nil {
		t.Fatal(err)
	}
	if p, ok := warm.Plan("new-1"); !ok || p.Title != "Cached plan" {
		t.Fatalf("Plan(new-1) from cache = %+v, %v", p, ok)
	}
	if s, ok := warm.Session("new-1"); !ok || s.Source != sourceCodex || s.Messages != 2 {
		t.Fatalf("Session(new-1) from cache = %+v, %v", s, ok)
	}
}

func TestCatalog(t *testing.T) {
	plansDir, claudeRoot := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(plansDir, "brave-fox.md"), []byte("# Claude plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(claudeRoot, "-home-u-dev-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	appendFile(t, filepath.Join(proj, "claude-1.jsonl"), userLine("2026-09-03T10:00:00Z", "claude prompt"))
	_, sessionsDir := codexFixture(t)
	appendFile(t, rollout(sessionsDir, "codex-1"), codexMeta("codex-1", "cli")+
		codexMessage("2026-09-02T10:00:00Z", "UserMessage", "codex prompt")+
		codexPlan("2099-01-01T00:00:00Z", "# Codex plan"))

	claude := NewTranscriptIndex(claudeRoot, 0)
	c := NewCatalog(NewStore(plansDir, claude), claude, nil, NewCodexIndex(sessionsDir, 0))
	if got := c.Sources(); !slices.Equal(got, []string{sourceClaude, sourceCodex}) {
		t.Fatalf("Sources() = %v", got)
	}

	plans, err := c.Plans()
	if err != nil || len(plans) != 2 || plans[0].Name != "codex-1" || plans[1].Name != "brave-fox.md" || plans[1].Source != sourceClaude {
		t.Fatalf("Plans() = %+v, %v", plans, err)
	}
	if p, err := c.Plan("codex-1"); err != nil || p.Title != "Codex plan" {
		t.Fatalf("Plan(codex-1) = %+v, %v", p, err)
	}
	if p, err := c.Plan("brave-fox.md"); err != nil || p.Title != "Claude plan" {
		t.Fatalf("Plan(brave-fox.md) = %+v, %v", p, err)
	}
	for _, name := range []string{"missing", "missing.md", "../brave-fox.md"} {
		if _, err := c.Plan(name); err != errNotFound {
			t.Errorf("Plan(%q) err = %v, want errNotFound", name, err)
		}
	}

	sessions := c.Sessions()
	if len(sessions) != 2 || sessions[0].ID != "codex-1" || sessions[0].Source != sourceCodex || sessions[1].ID != "claude-1" {
		t.Fatalf("Sessions() = %+v", sessions)
	}
	if s, ok := c.Session("codex-1"); !ok || s.agentName() != "Codex" {
		t.Fatalf("Session(codex-1) = %+v, %v", s, ok)
	}

	if got := NewCatalog(nil, NewCodexIndex(sessionsDir, 0)).Sources(); !slices.Equal(got, []string{sourceCodex}) {
		t.Fatalf("codex-only Sources() = %v", got)
	}
	if _, err := NewCatalog(nil, NewCodexIndex(sessionsDir, 0)).Plan("brave-fox.md"); err != errNotFound {
		t.Fatalf("Plan without store err = %v", err)
	}
}
