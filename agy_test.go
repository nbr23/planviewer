package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	agyMain     = "11111111-1111-4111-8111-111111111111"
	agySub      = "22222222-2222-4222-8222-222222222222"
	agyInternal = "33333333-3333-4333-8333-333333333333"
	agyRotated  = "44444444-4444-4444-8444-444444444444"
	agyBare     = "55555555-5555-4555-8555-555555555555"
)

type agyTestStep struct {
	StepIndex int    `json:"step_index"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Content   string `json:"content,omitempty"`
	ToolCalls []any  `json:"tool_calls,omitempty"`
}

func agyLog(steps ...agyTestStep) string {
	var b strings.Builder
	for _, s := range steps {
		line, _ := json.Marshal(s)
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func agyUser(i int, ts, text string) agyTestStep {
	return agyTestStep{i, "USER_EXPLICIT", "USER_INPUT", ts,
		"<USER_REQUEST>\n" + text + "\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe current local time is: now.\n</ADDITIONAL_METADATA>", nil}
}

func agyReply(i int, ts, text string) agyTestStep {
	return agyTestStep{i, "MODEL", "PLANNER_RESPONSE", ts, text, nil}
}

func agyWrite(i int, ts, target string) agyTestStep {
	return agyTestStep{i, "MODEL", "PLANNER_RESPONSE", ts, "", []any{map[string]any{
		"name": "write_to_file",
		"args": map[string]any{"TargetFile": target, "ArtifactMetadata": `{"Summary":"plan"}`, "CodeContent": `"# Plan"`},
	}}}
}

func agyReviewWrite(i int, ts, target string) agyTestStep {
	return agyTestStep{i, "MODEL", "PLANNER_RESPONSE", ts, "", []any{map[string]any{
		"name": "write_to_file",
		"args": map[string]any{"TargetFile": target, "ArtifactMetadata": map[string]any{"RequestFeedback": true, "Summary": "plan"}, "CodeContent": "# Plan"},
	}}}
}

func agyTool(i int, ts, name string) agyTestStep {
	return agyTestStep{i, "MODEL", "PLANNER_RESPONSE", ts, "", []any{map[string]any{"name": name, "args": map[string]any{}}}}
}

func writeTestFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func agyLogPath(dir, id, name string) string {
	return filepath.Join(dir, "brain", id, ".system_generated", "logs", name)
}

func agyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	longReply := "Full reply " + strings.Repeat("rug ", 1500) + "THE END"
	mainSteps := []agyTestStep{
		agyUser(0, "2026-09-01T10:00:00Z", "plan the rug cleaning"),
		agyWrite(1, "2026-09-01T10:00:01Z", "/home/u/.gemini/antigravity-cli/brain/"+agyMain+"/implementation_plan.md"),
		{2, "MODEL", "GENERIC", "2026-09-01T10:00:02Z", "Created the following subagents:\n{\n  \"conversationId\": \"" + agySub + "\"\n}", nil},
		agyReply(3, "2026-09-01T10:00:03Z", longReply),
	}
	writeTestFile(t, agyLogPath(dir, agyMain, "transcript_full.jsonl"), agyLog(mainSteps...))
	mainSteps[3] = agyReply(3, "2026-09-01T10:00:03Z", longReply[:4000])
	writeTestFile(t, agyLogPath(dir, agyMain, "transcript.jsonl"), agyLog(mainSteps...))

	writeTestFile(t, agyLogPath(dir, agySub, "transcript.jsonl"), agyLog(
		agyTestStep{0, "SYSTEM", "SYSTEM_MESSAGE", "2026-09-01T10:00:02Z", "sender=" + agyMain, nil},
		agyReply(1, "2026-09-01T10:00:05Z", "subagent reply"),
	))
	writeTestFile(t, agyLogPath(dir, agyInternal, "transcript.jsonl"), agyLog(agyUser(0, "2026-09-01T11:00:00Z", "internal prompt")))

	writeTestFile(t, agyLogPath(dir, agyRotated, "transcript_full.jsonl"), agyLog(agyUser(5, "2026-08-01T09:05:00Z", "rotated prompt")))
	writeTestFile(t, agyLogPath(dir, agyRotated, "transcript.jsonl"), agyLog(
		agyUser(0, "2026-08-01T09:00:00Z", "original prompt"),
		agyWrite(1, "2026-08-01T09:00:01Z", `"/home/u/.gemini/antigravity-cli/brain/`+agyRotated+`/task.md"`),
		agyReply(2, "2026-08-01T09:00:02Z", "ok"),
	))
	writeTestFile(t, agyLogPath(dir, agyBare, "transcript.jsonl"), agyLog(agyUser(0, "2026-07-01T09:00:00Z", "bare prompt")))

	meta := map[string]any{"conversations": map[string]any{
		agyMain: map[string]any{"is_internal": false, "summary": map[string]any{
			"Title": "Metadata title", "Preview": "plan the rug cleaning", "WorkspaceURIs": []string{"file:///home/u/dev/app"},
		}},
		agyRotated: map[string]any{"is_internal": false, "summary": map[string]any{
			"Title": "", "Preview": strings.Repeat("long preview ", 10), "WorkspaceURIs": []string{},
		}},
		agyInternal: map[string]any{"is_internal": true},
	}}
	raw, _ := json.Marshal(meta)
	writeTestFile(t, filepath.Join(dir, "cache", "conversation_metadata.json"), string(raw))
	writeTestFile(t, filepath.Join(dir, "annotations", agyMain+".pbtxt"), `title:"Rug \"deep\" clean"`)
	writeTestFile(t, filepath.Join(dir, "history.jsonl"), jsonLine(map[string]any{"display": "original prompt", "workspace": "/home/u/dev/other", "conversationId": agyRotated}))

	writeTestFile(t, filepath.Join(dir, "brain", agyMain, "implementation_plan.md"), "# Rug plan\n\nVacuum first.\n")
	writeTestFile(t, filepath.Join(dir, "brain", agyMain, "implementation_plan.md.metadata.json"), `{"summary":"plan","updatedAt":"2026-09-01T10:30:00.5Z"}`)
	writeTestFile(t, filepath.Join(dir, "brain", agyMain, "notes.md"), "# Not an artifact\n")
	writeTestFile(t, filepath.Join(dir, "brain", agyRotated, "task.md"), "# Tasks\n")
	writeTestFile(t, filepath.Join(dir, "brain", agyRotated, "task.md.metadata.json"), `{"updatedAt":"2026-08-01T09:30:00Z"}`)
	return dir
}

func TestAgyRoot(t *testing.T) {
	gemini := t.TempDir()
	cli := filepath.Join(gemini, "antigravity-cli")
	if err := os.MkdirAll(filepath.Join(cli, "brain"), 0o755); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{gemini: cli, cli: cli, t.TempDir(): "", "": ""} {
		if got := agyRoot(in); got != want {
			t.Errorf("agyRoot(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAgyIndex(t *testing.T) {
	dir := agyFixture(t)
	idx := NewAgyIndex(dir, 0)
	idx.Refresh()

	sessions := idx.Sessions()
	var ids []string
	for _, s := range sessions {
		ids = append(ids, s.ID)
	}
	if !slices.Equal(ids, []string{agyMain, agyRotated, agyBare}) {
		t.Fatalf("Sessions() = %v", ids)
	}
	main := sessions[0]
	if main.Source != sourceAgy || main.Title != `Rug "deep" clean` || main.Cwd != "/home/u/dev/app" || main.Project != "app" || main.Messages != 2 {
		t.Fatalf("main session = %+v", main)
	}
	if got := main.resumeCommand("/home/u"); got != "cd ~/dev/app && agy --conversation="+agyMain {
		t.Fatalf("resumeCommand = %q", got)
	}
	if main.agentName() != "Antigravity" {
		t.Fatalf("agentName = %q", main.agentName())
	}
	turns := sessionTexts(t, idx, agyMain)
	if turns[0] != "plan the rug cleaning" || !strings.HasSuffix(turns[1], "THE END") {
		t.Fatalf("main turns = %.80q", turns)
	}
	if full, _ := idx.Session(agyMain); strings.Contains(string(full.body), "current local time") {
		t.Fatal("ADDITIONAL_METADATA leaked into the conversation")
	}

	rotated := sessions[1]
	if !strings.HasPrefix(rotated.Title, "long preview long preview") || !strings.HasSuffix(rotated.Title, "…") || rotated.Cwd != "/home/u/dev/other" {
		t.Fatalf("rotated session = %+v", rotated)
	}
	if got := sessionTexts(t, idx, agyRotated); !slices.Equal(got, []string{"original prompt", "ok"}) {
		t.Fatalf("rotated turns = %q, want transcript.jsonl content", got)
	}
	bare := sessions[2]
	if bare.Title != "bare prompt" || bare.Cwd != "" || bare.resumeCommand("/home/u") != "agy --conversation="+agyBare {
		t.Fatalf("bare session = %+v", bare)
	}
	for _, id := range []string{agySub, agyInternal} {
		if _, ok := idx.Session(id); ok {
			t.Errorf("Session(%s) should be hidden", id)
		}
	}

	writeTestFile(t, filepath.Join(dir, "annotations", agyMain+".pbtxt"), `title:"Persian rug cleaning"`)
	idx.Refresh()
	if s, _ := idx.Session(agyMain); s.Title != "Persian rug cleaning" {
		t.Fatalf("title after annotation change = %q", s.Title)
	}
}

func TestAgyArtifacts(t *testing.T) {
	dir := agyFixture(t)
	idx := NewAgyIndex(dir, 0)
	store := newArtifactStore(idx)

	plans, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if got := planNames(plans); !slices.Equal(got, []string{agyMain + "/implementation_plan.md", agyRotated + "/task.md"}) {
		t.Fatalf("List() = %v", got)
	}
	p, err := store.Get(agyMain + "/implementation_plan.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != sourceAgy || p.Title != "Rug plan" || p.Project != "app" || !p.CreatedExact ||
		!p.Created.Equal(time.Date(2026, 9, 1, 10, 0, 1, 0, time.UTC)) || !p.Updated.Equal(time.Date(2026, 9, 1, 10, 30, 0, 5e8, time.UTC)) {
		t.Fatalf("plan = %+v", p)
	}
	task, err := store.Get(agyRotated + "/task.md")
	if err != nil || !task.CreatedExact || !task.Created.Equal(time.Date(2026, 8, 1, 9, 0, 1, 0, time.UTC)) || task.Project != "other" {
		t.Fatalf("task = %+v, %v", task, err)
	}
	for _, name := range []string{"../x.md", agyMain + "/../y.md", "not-a-uuid/implementation_plan.md", agyMain + "/notes.md", agyMain, agyMain + "/sub/implementation_plan.md"} {
		if _, err := store.Get(name); err != errNotFound {
			t.Errorf("Get(%q) err = %v, want errNotFound", name, err)
		}
	}

	c := NewCatalog(nil, store, idx)
	if p, err := c.Plan(agyMain + "/implementation_plan.md"); err != nil || p.Title != "Rug plan" {
		t.Fatalf("Catalog.Plan = %+v, %v", p, err)
	}
	if plans, err := c.Plans(); err != nil || len(plans) != 2 || plans[0].Name != agyMain+"/implementation_plan.md" {
		t.Fatalf("Catalog.Plans = %v, %v", planNames(plans), err)
	}
}

func TestAgyPlanStatus(t *testing.T) {
	dir := t.TempDir()
	brain := "/home/u/.gemini/antigravity-cli/brain/" + agyMain + "/"
	log := agyLogPath(dir, agyMain, "transcript.jsonl")
	writeTestFile(t, log, agyLog(
		agyUser(0, "2026-09-01T10:00:00Z", "plan the rug cleaning"),
		agyReviewWrite(1, "2026-09-01T10:00:01Z", brain+"implementation_plan.md"),
		agyWrite(2, "2026-09-01T10:00:02Z", brain+"task.md"),
	))
	for _, f := range []string{"implementation_plan.md", "task.md"} {
		writeTestFile(t, filepath.Join(dir, "brain", agyMain, f), "# "+f+"\n")
		writeTestFile(t, filepath.Join(dir, "brain", agyMain, f+".metadata.json"), "{}")
	}
	store := newArtifactStore(NewAgyIndex(dir, 0))
	status := func(file string) planStatus {
		t.Helper()
		p, err := store.Get(agyMain + "/" + file)
		if err != nil {
			t.Fatal(err)
		}
		return p.Status
	}

	if got := status("implementation_plan.md"); got != statusPending {
		t.Fatalf("initial status = %q, want pending", got)
	}
	steps := []struct {
		step agyTestStep
		want planStatus
	}{
		{agyReply(3, "2026-09-01T10:00:03Z", "Review the plan."), statusPending},
		{agyUser(4, "2026-09-01T10:01:00Z", "why vacuum first?"), statusPending},
		{agyReviewWrite(5, "2026-09-01T10:01:30Z", brain+"implementation_plan.md"), statusPending},
		{agyTool(6, "2026-09-01T10:01:31Z", "view_file"), statusPending},
		{agyUser(7, "2026-09-01T10:02:00Z", "[Approved] implementation_plan.md"), statusApproved},
		{agyTool(8, "2026-09-01T10:02:01Z", "view_file"), statusWorked},
		{agyReviewWrite(9, "2026-09-01T11:00:00Z", brain+"implementation_plan.md"), statusPending},
		{agyUser(10, "2026-09-01T11:01:00Z", "let's go"), statusPending},
		{agyTool(11, "2026-09-01T11:01:01Z", "replace_file_content"), statusWorked},
	}
	for i, s := range steps {
		appendFile(t, log, agyLog(s.step))
		if got := status("implementation_plan.md"); got != s.want {
			t.Fatalf("step %d: status = %q, want %q", i, got, s.want)
		}
	}
	if got := status("task.md"); got != "" {
		t.Fatalf("task.md status = %q, want none", got)
	}
}

func TestAgyRequestsFeedback(t *testing.T) {
	for raw, want := range map[string]bool{
		`{"RequestFeedback":true,"Summary":"plan"}`: true,
		`"{\"RequestFeedback\":true}"`:              true,
		`{"Summary":"plan"}`:                        false,
		`"{\"Summary\":\"plan\"}"`:                  false,
		`null`:                                      false,
	} {
		if got := agyRequestsFeedback(json.RawMessage(raw)); got != want {
			t.Errorf("agyRequestsFeedback(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestAgyCacheWarmStart(t *testing.T) {
	dir := agyFixture(t)
	cache := filepath.Join(t.TempDir(), "index.gob")
	cold := NewAgyIndex(dir, 0)
	cold.EnableCache(cache)
	cold.Refresh()

	warm := NewAgyIndex(dir, time.Hour)
	warm.EnableCache(cache)
	if err := warm.CacheErr(); err != nil {
		t.Fatal(err)
	}
	if _, ok := warm.Session(agySub); ok {
		t.Fatal("subagent visible after warm start")
	}
	if _, ok := warm.Session(agyMain); !ok {
		t.Fatal("main session not restored")
	}
	if _, ok := warm.Lookup(agyMain + "/implementation_plan.md"); !ok {
		t.Fatal("artifact origin not restored")
	}
}
