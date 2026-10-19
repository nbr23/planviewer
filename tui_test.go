package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		args     []string
		cmd      string
		rest     []string
		wantFail bool
	}{
		{nil, "tui", nil, false},
		{[]string{"-claude-dir", "x"}, "tui", []string{"-claude-dir", "x"}, false},
		{[]string{"tui", "-claude-dir", "x"}, "tui", []string{"-claude-dir", "x"}, false},
		{[]string{"serve", "-addr", ":1"}, "serve", []string{"-addr", ":1"}, false},
		{[]string{"bogus"}, "", nil, true},
	}
	for _, tt := range tests {
		cmd, rest, err := parseCommand(tt.args)
		if (err != nil) != tt.wantFail || cmd != tt.cmd || !slices.Equal(rest, tt.rest) {
			t.Errorf("parseCommand(%q) = %q, %q, %v", tt.args, cmd, rest, err)
		}
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{time.Minute, "1m ago"},
		{59 * time.Minute, "59m ago"},
		{time.Hour, "1h ago"},
		{23*time.Hour + 59*time.Minute, "23h ago"},
		{24 * time.Hour, "1d ago"},
		{6 * 24 * time.Hour, "6d ago"},
		{7 * 24 * time.Hour, "Oct 2"},
		{300 * 24 * time.Hour, "Dec 13, 2025"},
	}
	for _, tt := range tests {
		if got := relativeTime(now.Add(-tt.ago), now); got != tt.want {
			t.Errorf("relativeTime(-%s) = %q, want %q", tt.ago, got, tt.want)
		}
	}
}

func TestSortPlans(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	plans := []Plan{
		{Name: "a.md", Title: "banana", Project: "", Size: 30, Created: base.Add(2 * time.Hour), Updated: base.Add(3 * time.Hour)},
		{Name: "b.md", Title: "Apple", Project: "zeta", Size: 10, Created: base.Add(1 * time.Hour), Updated: base.Add(5 * time.Hour)},
		{Name: "c.md", Title: "cherry", Project: "Alpha", Size: 20, Created: base.Add(3 * time.Hour), Updated: base.Add(4 * time.Hour)},
		{Name: "d.md", Title: "apple", Project: "zeta", Size: 10, Created: base, Updated: base.Add(6 * time.Hour)},
	}
	tests := []struct {
		key  string
		desc bool
		want []string
	}{
		{"updated", true, []string{"d.md", "b.md", "c.md", "a.md"}},
		{"updated", false, []string{"a.md", "c.md", "b.md", "d.md"}},
		{"created", true, []string{"c.md", "a.md", "b.md", "d.md"}},
		{"title", false, []string{"d.md", "b.md", "a.md", "c.md"}},
		{"project", false, []string{"a.md", "c.md", "d.md", "b.md"}},
		{"size", true, []string{"a.md", "c.md", "d.md", "b.md"}},
	}
	for _, tt := range tests {
		got := slices.Clone(plans)
		sortPlans(got, tt.key, tt.desc)
		var names []string
		for _, p := range got {
			names = append(names, p.Name)
		}
		if !slices.Equal(names, tt.want) {
			t.Errorf("sortPlans(%s, desc=%v) = %v, want %v", tt.key, tt.desc, names, tt.want)
		}
	}
}

func update(t *testing.T, m model, msgs ...tea.Msg) model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(model)
	}
	return m
}

func typeText(s string) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func shownNames(m model) []string {
	var names []string
	for _, it := range m.shown {
		names = append(names, it.itemKey())
	}
	return names
}

func TestModelSearchOpenAndBack(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"rugs.md":  "# Persian rugs\n\nThe fringe catches on the brush roll.\n",
		"feed.md":  "# Goodreads live feed\n\nnothing here\n",
		"other.md": "# Unrelated\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(dir, nil)
	plans, err := store.List()
	if err != nil {
		t.Fatal(err)
	}

	m := update(t, newModel(NewCatalog(store), "notty"),
		tea.WindowSizeMsg{Width: 100, Height: 30},
		loadedMsg{plans: plans})
	if len(m.shown) != 3 {
		t.Fatalf("shown = %v, want 3 plans", shownNames(m))
	}

	m = update(t, m, typeText("brush"))
	if got := shownNames(m); !slices.Equal(got, []string{"rugs.md"}) || m.shown[0].itemSnippet() == "" {
		t.Fatalf("after search shown = %v snippet=%q", got, m.shown[0].itemSnippet())
	}
	for _, p := range m.plans {
		if p.Snippet != "" {
			t.Fatalf("search mutated all: %s snippet=%q", p.Name, p.Snippet)
		}
	}
	if v := m.View(); !strings.Contains(v, "fringe catches") || !strings.Contains(v, "1 of 3") {
		t.Fatalf("list view missing snippet or count:\n%s", v)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != pagerScreen {
		t.Fatal("enter did not open the plan")
	}
	if v := m.View(); !strings.Contains(v, "Persian rugs") || !strings.Contains(v, "match 1/1") {
		t.Fatalf("plan view:\n%s", v)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != listScreen || m.input.Value() != "brush" {
		t.Fatalf("esc from plan: screen=%v query=%q", m.screen, m.input.Value())
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.input.Value() != "" || len(m.shown) != 3 {
		t.Fatalf("esc in list: query=%q shown=%v", m.input.Value(), shownNames(m))
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd == nil || cmd() != tea.Quit() {
		t.Fatal("esc on empty query did not quit")
	}
}

func TestSortSessions(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sessions := []Session{
		{ID: "a", Title: "beta", Messages: 3, Updated: base.Add(time.Hour)},
		{ID: "b", Title: "Alpha", Messages: 9, Updated: base},
		{ID: "c", Title: "gamma", Messages: 1, Updated: base.Add(2 * time.Hour)},
	}
	tests := []struct {
		key  string
		desc bool
		want []string
	}{
		{"updated", true, []string{"c", "a", "b"}},
		{"title", false, []string{"b", "a", "c"}},
		{"messages", true, []string{"b", "a", "c"}},
	}
	for _, tt := range tests {
		got := slices.Clone(sessions)
		sortSessions(got, tt.key, tt.desc)
		var ids []string
		for _, s := range got {
			ids = append(ids, s.ID)
		}
		if !slices.Equal(ids, tt.want) {
			t.Errorf("sortSessions(%s, desc=%v) = %v, want %v", tt.key, tt.desc, ids, tt.want)
		}
	}
}

func TestModelSessionsSearchOpenAndResume(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	plansDir, root := t.TempDir(), t.TempDir()
	proj := filepath.Join(root, "-home-u-dev-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	appendFile(t, filepath.Join(proj, "sess-1.jsonl"),
		userLine("2026-09-01T10:00:00Z", "why does the vacuum stall?")+
			assistantLine("2026-09-01T10:00:01Z", "text", "The fringe tangles around the brushroll.")+
			jsonLine(map[string]any{"type": "ai-title", "aiTitle": "Vacuum stalls"}))
	appendFile(t, filepath.Join(proj, "sess-2.jsonl"), userLine("2026-09-02T10:00:00Z", "unrelated"))

	idx := NewTranscriptIndex(root, 0)
	store := NewStore(plansDir, idx)
	m := newModel(NewCatalog(store, idx), "notty")
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, m.load()())
	if len(m.shown) != 0 || m.mode != plansMode {
		t.Fatalf("plans mode shown = %v", shownNames(m))
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.mode != sessionsMode || len(m.shown) != 2 || !strings.Contains(m.View(), "2 sessions") {
		t.Fatalf("after ctrl+t mode=%v shown=%v\n%s", m.mode, shownNames(m), m.View())
	}

	m = update(t, m, typeText("brushroll"))
	if got := shownNames(m); !slices.Equal(got, []string{"sess-1"}) || m.shown[0].itemSnippet() == "" {
		t.Fatalf("after search shown = %v", got)
	}
	if v := m.View(); !strings.Contains(v, "Vacuum stalls") || !strings.Contains(v, "1 of 2 sessions") {
		t.Fatalf("list view:\n%s", v)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	v := ansi.Strip(m.View())
	if m.screen != pagerScreen || !strings.Contains(v, "match 1/1") || !strings.Contains(v, "r resume") {
		t.Fatalf("session view:\n%s", v)
	}
	if !strings.Contains(v, "> why does the vacuum stall?") || !strings.Contains(v, replyBullet+" The fringe tangles") {
		t.Fatalf("session view is not a conversation:\n%s", v)
	}
	header := strings.Split(v, "\n")
	if len(header) != 30 || strings.TrimSpace(header[2]) != "cd ~/dev/app && claude --resume sess-1" {
		t.Fatalf("view has %d lines, want 30, resume command on line 3:\n%s", len(header), v)
	}

	next, cmd := m.Update(typeText("r"))
	m = next.(model)
	if cmd == nil || cmd() != tea.Quit() || m.resume != "cd ~/dev/app && claude --resume sess-1" {
		t.Fatalf("r: resume=%q", m.resume)
	}
}

func TestModelToggleWithoutTranscripts(t *testing.T) {
	m := update(t, newModel(NewCatalog(NewStore(t.TempDir(), nil)), "notty"),
		tea.WindowSizeMsg{Width: 100, Height: 30},
		tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.mode != plansMode || m.status == "" {
		t.Fatalf("ctrl+t without transcripts: mode=%v status=%q", m.mode, m.status)
	}
}

func TestModelMixedSources(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	plansDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(plansDir, "brave-fox.md"), []byte("# Claude plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sessionsDir := codexFixture(t)
	appendFile(t, rollout(sessionsDir, "codex-1"), codexMeta("codex-1", "cli")+
		codexMessage("2026-09-02T10:00:00Z", "UserMessage", "plan the rugs")+
		codexPlan("2026-09-02T10:00:01Z", "# Rug plan\n\nVacuum first."))

	if newModel(NewCatalog(NewStore(plansDir, nil)), "notty").labelSources {
		t.Fatal("single source is labelled")
	}
	m := newModel(NewCatalog(NewStore(plansDir, nil), NewCodexIndex(sessionsDir, 0)), "notty")
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, m.load()())
	if got := shownNames(m); !slices.Equal(got, []string{"brave-fox.md", "codex-1"}) {
		t.Fatalf("plans shown = %v", got)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "claude  brave-fox.md") || !strings.Contains(v, "codex  app  codex-1") {
		t.Fatalf("list view lacks source labels:\n%s", v)
	}

	m = update(t, m, typeText("src:codex"))
	if got := shownNames(m); !slices.Equal(got, []string{"codex-1"}) {
		t.Fatalf("src:codex shown = %v", got)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if v := ansi.Strip(m.View()); m.screen != pagerScreen || !strings.Contains(v, "Rug plan") || !strings.Contains(v, "codex · codex-1 · app") {
		t.Fatalf("codex plan view:\n%s", v)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc}, tea.KeyMsg{Type: tea.KeyEsc}, tea.KeyMsg{Type: tea.KeyCtrlT})
	if got := shownNames(m); m.mode != sessionsMode || !slices.Equal(got, []string{"codex-1"}) {
		t.Fatalf("sessions shown = %v", got)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	next, cmd := m.Update(typeText("r"))
	if m = next.(model); cmd == nil || cmd() != tea.Quit() || m.resume != "cd ~/dev/app && codex resume codex-1" {
		t.Fatalf("r: resume=%q", m.resume)
	}
}

func TestSpread(t *testing.T) {
	tests := []struct {
		left, right string
		width       int
		want        string
	}{
		{"abc", "xy", 10, "abc     xy"},
		{"abcdefghij", "xy", 8, "abcd… xy"},
	}
	for _, tt := range tests {
		if got := spread(tt.left, tt.right, tt.width); got != tt.want {
			t.Errorf("spread(%q, %q, %d) = %q, want %q", tt.left, tt.right, tt.width, got, tt.want)
		}
	}
}

func TestItemLines(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	m := update(t, newModel(NewCatalog(NewStore(t.TempDir(), nil)), "notty"), tea.WindowSizeMsg{Width: 60, Height: 30})
	s := Session{ID: "a", Title: "Vacuum stalls", Project: "app", Branch: "main",
		Created: now.Add(-48 * time.Hour), Updated: now.Add(-2 * time.Hour), Messages: 12}

	lines := m.itemLines(s, false, now)
	for i, l := range lines[:2] {
		lines[i] = ansi.Strip(l)
		if w := ansi.StringWidth(lines[i]); w != 60 {
			t.Errorf("line %d width = %d, want 60: %q", i, w, lines[i])
		}
	}
	if !strings.HasPrefix(lines[0], "  Vacuum stalls ") || !strings.HasSuffix(lines[0], " 2h ago") {
		t.Errorf("title line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  app  main ") || !strings.HasSuffix(lines[1], " 12 messages · created 2d ago") {
		t.Errorf("meta line = %q", lines[1])
	}

	s.Title = strings.Repeat("x", 100)
	if l := ansi.Strip(m.itemLines(s, false, now)[0]); !strings.Contains(l, "x… ") || !strings.HasSuffix(l, " 2h ago") {
		t.Errorf("long title line = %q", l)
	}
}
