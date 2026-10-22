package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

func jsonLine(rec map[string]any) string {
	b, _ := json.Marshal(rec)
	return string(b) + "\n"
}

func userLine(ts, text string) string {
	return jsonLine(map[string]any{
		"type": "user", "timestamp": ts, "cwd": "/home/u/dev/app", "gitBranch": "main",
		"message": map[string]any{"role": "user", "content": text},
	})
}

func assistantLine(ts, blockType, text string) string {
	return jsonLine(map[string]any{
		"type": "assistant", "timestamp": ts, "cwd": "/home/u/dev/app", "gitBranch": "feature",
		"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": blockType, blockType: text}}},
	})
}

func TestSessionIndex(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-home-u-dev-app")
	subDir := filepath.Join(proj, "sess-1", "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(proj, "sess-1.jsonl")
	appendFile(t, session, strings.Join([]string{
		userLine("2026-09-01T10:00:00Z", "<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>"),
		userLine("2026-09-01T10:00:01Z", "<local-command-caveat>Caveat: ignore</local-command-caveat>"),
		userLine("2026-09-01T10:00:02Z", "<command-name>/plan</command-name>\n<command-message>plan</command-message>\n<command-args>vacuum the persian rugs</command-args>"),
		jsonLine(map[string]any{"type": "ai-title", "aiTitle": "Rug vacuuming"}),
		assistantLine("2026-09-01T10:00:03Z", "thinking", "secret reasoning"),
		assistantLine("2026-09-01T10:00:04Z", "text", "The fringe catches on the brush roll."),
		assistantLine("2026-09-01T10:00:05Z", "text", "Lower the suction."),
		jsonLine(map[string]any{
			"type": "user", "timestamp": "2026-09-01T10:00:06Z",
			"message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "x", "content": "tool output"}}},
		}),
		jsonLine(map[string]any{"type": "user", "isSidechain": true, "timestamp": "2026-09-01T10:00:07Z", "message": map[string]any{"content": "sidechain prompt"}}),
		jsonLine(map[string]any{"type": "user", "isMeta": true, "timestamp": "2026-09-01T10:00:08Z", "message": map[string]any{"content": "meta prompt"}}),
		userLine("2026-09-01T10:00:09Z", "thanks<system-reminder>hidden reminder</system-reminder>"),
		jsonLine(map[string]any{"type": "custom-title", "customTitle": "Persian rugs"}),
	}, ""))
	appendFile(t, filepath.Join(subDir, "agent-1.jsonl"), userLine("2026-09-01T10:00:00Z", "subagent prompt"))

	idx := NewTranscriptIndex(root, 0)
	idx.Refresh()

	sessions := idx.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("Sessions() = %+v, want 1", sessions)
	}
	s := sessions[0]
	if s.ID != "sess-1" || s.Title != "Persian rugs" || s.Project != "app" || s.Cwd != "/home/u/dev/app" || s.Branch != "main" || s.Messages != 3 {
		t.Fatalf("session = %+v", s)
	}
	if !s.Created.Equal(time.Date(2026, 9, 1, 10, 0, 2, 0, time.UTC)) || !s.Updated.Equal(time.Date(2026, 9, 1, 10, 0, 9, 0, time.UTC)) {
		t.Fatalf("created=%s updated=%s", s.Created, s.Updated)
	}

	full, ok := idx.Session("sess-1")
	if !ok {
		t.Fatal("Session(sess-1) not found")
	}
	want := []string{"/plan vacuum the persian rugs", "The fringe catches on the brush roll.\n\nLower the suction.", "thanks"}
	var got []string
	for _, tr := range full.turns {
		got = append(got, tr.text)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("turns = %q, want %q", got, want)
	}
	for _, hidden := range []string{"secret reasoning", "tool output", "sidechain", "meta prompt", "hidden reminder", "Caveat", "subagent"} {
		if bytes.Contains(full.body, []byte(hidden)) {
			t.Errorf("body contains %q", hidden)
		}
	}

	appendFile(t, session, assistantLine("2026-09-01T11:00:00Z", "text", "You're welcome."))
	idx.Refresh()
	if s, _ := idx.Session("sess-1"); s.Messages != 4 || len(full.turns) != 3 {
		t.Fatalf("after append: messages=%d, earlier snapshot turns=%d", s.Messages, len(full.turns))
	}

	os.Remove(session)
	idx.Refresh()
	if ss := idx.Sessions(); len(ss) != 0 {
		t.Fatalf("deleted session still listed: %+v", ss)
	}
}

func TestSessionTitleFallback(t *testing.T) {
	b := &sessionBuilder{id: "s"}
	long := strings.Repeat("word ", 30)
	b.add(&transcriptRecord{Type: "user", Message: struct {
		Content json.RawMessage `json:"content"`
	}{Content: json.RawMessage(strconv.Quote("  first\n\n" + long))}})
	title := b.snapshot(false).Title
	if !strings.HasPrefix(title, "first word") || !strings.HasSuffix(title, "…") || utf8.RuneCountInString(title) != titleMaxLen+1 {
		t.Fatalf("title = %q", title)
	}
}

func TestPromptText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain prompt", "plain prompt"},
		{"<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>", ""},
		{"<command-message>plan</command-message>\n<command-name>/plan</command-name>\n<command-args>do it</command-args>", "/plan do it"},
		{"<local-command-stdout>output</local-command-stdout>", ""},
		{"before<system-reminder>a\nb</system-reminder> after", "before after"},
		{"[Request interrupted by user for tool use]", ""},
	}
	for _, tt := range tests {
		if got := promptText(tt.in); got != tt.want {
			t.Errorf("promptText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResumeCommand(t *testing.T) {
	tests := []struct {
		cwd, home, want string
	}{
		{"/home/u/dev/app", "/home/u", "cd ~/dev/app && claude --resume abc-1"},
		{"/home/u/dev/app", "/home/u/", "cd ~/dev/app && claude --resume abc-1"},
		{"/home/u", "/home/u", "cd ~ && claude --resume abc-1"},
		{"/home/u/my app's", "/home/u", `cd ~/'my app'\''s' && claude --resume abc-1`},
		{"/home/u2/app", "/home/u", "cd /home/u2/app && claude --resume abc-1"},
		{"/srv/my app", "/home/u", "cd '/srv/my app' && claude --resume abc-1"},
		{"/home/u/dev/app", "", "cd /home/u/dev/app && claude --resume abc-1"},
		{"/home/u/dev/app", "/", "cd /home/u/dev/app && claude --resume abc-1"},
		{"", "/home/u", "claude --resume abc-1"},
	}
	for _, tt := range tests {
		s := Session{ID: "abc-1", Cwd: tt.cwd}
		if got := s.resumeCommand(tt.home); got != tt.want {
			t.Errorf("resumeCommand(cwd=%q, home=%q) = %q, want %q", tt.cwd, tt.home, got, tt.want)
		}
	}
}

func TestFilterSessions(t *testing.T) {
	sessions := func() []Session {
		return []Session{
			{ID: "s1", Title: "Persian rugs", Project: "vacuum", body: []byte("The fringe catches on the BRUSH roll.")},
			{ID: "s2", Title: "Live feed", Branch: "goodreads-rss", body: []byte("nothing")},
		}
	}
	tests := []struct {
		q       string
		want    string
		snippet bool
	}{
		{"brush", "s1", true},
		{"goodreads", "s2", false},
		{"vacuum fringe", "s1", true},
		{"s2", "s2", false},
	}
	for _, tt := range tests {
		got := filterSessions(sessions(), parseQuery(tt.q))
		if len(got) != 1 || got[0].ID != tt.want || (got[0].Snippet != "") != tt.snippet {
			t.Errorf("q=%q got %+v, want %s snippet=%v", tt.q, got, tt.want, tt.snippet)
		}
	}
}

func cachedIndex(t *testing.T, root, cache string) *TranscriptIndex {
	t.Helper()
	idx := NewTranscriptIndex(root, 0)
	idx.EnableCache(cache)
	idx.Refresh()
	if err := idx.CacheErr(); err != nil {
		t.Fatal(err)
	}
	return idx
}

func cacheFixture(t *testing.T) (root, session, cache string) {
	t.Helper()
	root = t.TempDir()
	proj := filepath.Join(root, "-home-u-dev-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	session = filepath.Join(proj, "sess.jsonl")
	appendFile(t, session, userLine("2026-09-01T10:00:00Z", "original prompt")+writeRecord("2026-09-01T10:00:01Z", "/home/u/dev/app", "/h/.claude/plans/one.md"))
	return root, session, filepath.Join(t.TempDir(), "cache", "index.gob")
}

func sessionTexts(t *testing.T, idx *TranscriptIndex, id string) []string {
	t.Helper()
	s, ok := idx.Session(id)
	if !ok {
		t.Fatalf("Session(%s) not found", id)
	}
	var out []string
	for _, tr := range s.turns {
		out = append(out, tr.text)
	}
	return out
}

func claudeBlockLine(ts, typ string, block map[string]any) string {
	return jsonLine(map[string]any{
		"type": typ, "timestamp": ts, "cwd": "/home/u/dev/app",
		"message": map[string]any{"role": typ, "content": []any{block}},
	})
}

func exitPlanLine(ts, id, planFile string) string {
	input := map[string]any{}
	if planFile != "" {
		input["planFilePath"] = planFile
	}
	return claudeBlockLine(ts, "assistant", map[string]any{"type": "tool_use", "id": id, "name": "ExitPlanMode", "input": input})
}

func toolResultLine(ts, id string, isError bool) string {
	return claudeBlockLine(ts, "user", map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": isError, "content": "result"})
}

func toolUseLine(ts, name string) string {
	return claudeBlockLine(ts, "assistant", map[string]any{"type": "tool_use", "id": "toolu_" + name, "name": name, "input": map[string]any{}})
}

func TestClaudePlanStatus(t *testing.T) {
	plansDir, root := t.TempDir(), t.TempDir()
	proj := filepath.Join(root, "-home-u-dev-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"brave-fox.md", "calm-owl.md", "lone-elk.md"} {
		if err := os.WriteFile(filepath.Join(plansDir, name), []byte("# Plan\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx := NewTranscriptIndex(root, 0)
	store := NewStore(plansDir, idx)
	status := func(name string) planStatus {
		t.Helper()
		p, err := store.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		return p.Status
	}

	session := filepath.Join(proj, "sess.jsonl")
	blob := jsonLine(map[string]any{"type": "api-request-blob", "message": map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "tool_use", "id": "toolu_blob", "name": "ExitPlanMode", "input": map[string]any{"planFilePath": "/home/u/.claude/plans/brave-fox.md"}},
	}}})
	steps := []struct {
		line string
		want planStatus
	}{
		{writeRecord("2026-09-01T10:00:00Z", "/home/u/dev/app", "/home/u/.claude/plans/brave-fox.md"), statusDraft},
		{blob, statusDraft},
		{exitPlanLine("2026-09-01T10:00:01Z", "toolu_1", "/home/u/.claude/plans/brave-fox.md"), statusPending},
		{toolUseLine("2026-09-01T10:00:02Z", "Read"), statusPending},
		{toolResultLine("2026-09-01T10:00:03Z", "toolu_other", false), statusPending},
		{toolResultLine("2026-09-01T10:00:04Z", "toolu_1", false), statusApproved},
		{toolUseLine("2026-09-01T10:00:05Z", "Bash"), statusWorked},
		{exitPlanLine("2026-09-01T10:01:00Z", "toolu_2", "/home/u/.claude/plans/brave-fox.md"), statusPending},
	}
	for i, s := range steps {
		appendFile(t, session, s.line)
		if got := status("brave-fox.md"); got != s.want {
			t.Fatalf("step %d: status = %q, want %q", i, got, s.want)
		}
	}

	appendFile(t, filepath.Join(proj, "other.jsonl"), writeRecord("2026-09-02T10:00:00Z", "/home/u/dev/app", "/home/u/.claude/plans/calm-owl.md")+
		exitPlanLine("2026-09-02T10:00:01Z", "toolu_3", "")+
		toolResultLine("2026-09-02T10:00:02Z", "toolu_3", true)+
		toolUseLine("2026-09-02T10:00:03Z", "Edit"))
	if got := status("calm-owl.md"); got != statusRejected {
		t.Fatalf("calm-owl.md status = %q, want rejected", got)
	}
	if got := status("lone-elk.md"); got != "" {
		t.Fatalf("plan without transcript has status %q", got)
	}
}

func TestClaudePlanStatusResumesFromCache(t *testing.T) {
	root, session, cache := cacheFixture(t)
	if got := cachedIndex(t, root, cache).Status("one.md"); got != "" {
		t.Fatalf("status before ExitPlanMode = %q", got)
	}
	appendFile(t, session, exitPlanLine("2026-09-01T10:00:02Z", "toolu_1", ""))
	if got := cachedIndex(t, root, cache).Status("one.md"); got != statusPending {
		t.Fatalf("status after ExitPlanMode = %q, want pending", got)
	}
	appendFile(t, session, toolResultLine("2026-09-01T10:05:00Z", "toolu_1", false)+toolUseLine("2026-09-01T10:05:01Z", "Edit"))
	if got := cachedIndex(t, root, cache).Status("one.md"); got != statusWorked {
		t.Fatalf("status after approval and edit = %q, want worked on", got)
	}
}

func TestTranscriptCacheWarmStart(t *testing.T) {
	root, session, cache := cacheFixture(t)
	cachedIndex(t, root, cache)

	info, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("cache mode = %o, want 600", perm)
	}

	st, err := os.Stat(session)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session, bytes.ReplaceAll(data, []byte("original"), []byte("replaced")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(session, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}

	warm := cachedIndex(t, root, cache)
	if got := sessionTexts(t, warm, "sess"); got[0] != "original prompt" {
		t.Fatalf("turns = %q, want cached content", got)
	}
	if _, ok := warm.Lookup("one.md"); !ok {
		t.Fatal("origin not restored from cache")
	}
}

func TestTranscriptCacheResumesAppend(t *testing.T) {
	root, session, cache := cacheFixture(t)
	cachedIndex(t, root, cache)
	appendFile(t, session, assistantLine("2026-09-01T10:00:02Z", "text", "appended reply"))

	warm := cachedIndex(t, root, cache)
	cold := NewTranscriptIndex(root, 0)
	cold.Refresh()
	if got, want := sessionTexts(t, warm, "sess"), sessionTexts(t, cold, "sess"); !slices.Equal(got, want) {
		t.Fatalf("warm turns = %q, cold turns = %q", got, want)
	}
}

func TestTranscriptCacheRescansRewrittenFile(t *testing.T) {
	root, session, cache := cacheFixture(t)
	cachedIndex(t, root, cache)

	data, err := os.ReadFile(session)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := userLine("2026-09-02T10:00:00Z", "rewritten prompt") + strings.Repeat(" ", len(data)) + "\n"
	if err := os.WriteFile(session, []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}

	warm := cachedIndex(t, root, cache)
	if got := sessionTexts(t, warm, "sess"); !slices.Equal(got, []string{"rewritten prompt"}) {
		t.Fatalf("turns = %q, want rescanned content", got)
	}
	if _, ok := warm.Lookup("one.md"); !ok {
		t.Fatal("origin from earlier content was dropped")
	}
}

func TestTranscriptCacheDeletedTranscript(t *testing.T) {
	root, session, cache := cacheFixture(t)
	cachedIndex(t, root, cache)
	if err := os.Remove(session); err != nil {
		t.Fatal(err)
	}

	warm := cachedIndex(t, root, cache)
	if ss := warm.Sessions(); len(ss) != 0 {
		t.Fatalf("deleted session still listed: %+v", ss)
	}
	if _, ok := warm.Lookup("one.md"); !ok {
		t.Fatal("origin of deleted transcript was dropped")
	}
}

func TestTranscriptCacheIgnoresStaleCache(t *testing.T) {
	root, session, cache := cacheFixture(t)
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte("not a gob"), 0o600); err != nil {
		t.Fatal(err)
	}
	cachedIndex(t, root, cache)

	st, err := os.Stat(session)
	if err != nil {
		t.Fatal(err)
	}
	stale := indexCache{Build: "other", Root: root, Files: map[string]cachedTranscript{
		session: {ModTime: st.ModTime(), Offset: st.Size(), Session: &cachedSession{ID: "sess", Turns: []cachedTurn{{User: true, Text: "stale"}}}},
	}}
	if err := writeCache(cache, &stale); err != nil {
		t.Fatal(err)
	}
	idx := cachedIndex(t, root, cache)
	if got := sessionTexts(t, idx, "sess"); got[0] != "original prompt" {
		t.Fatalf("turns = %q, want fresh scan", got)
	}

	c, err := readCache(cache)
	if err != nil || c == nil || c.Build == "other" {
		t.Fatalf("cache not rewritten: %+v, %v", c, err)
	}
}
