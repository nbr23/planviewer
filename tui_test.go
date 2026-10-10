package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	for _, p := range m.shown {
		names = append(names, p.Name)
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

	m := update(t, newModel(store, false, "notty"),
		tea.WindowSizeMsg{Width: 100, Height: 30},
		plansMsg{plans: plans})
	if len(m.shown) != 3 {
		t.Fatalf("shown = %v, want 3 plans", shownNames(m))
	}

	m = update(t, m, typeText("brush"))
	if got := shownNames(m); !slices.Equal(got, []string{"rugs.md"}) || m.shown[0].Snippet == "" {
		t.Fatalf("after search shown = %v snippet=%q", got, m.shown[0].Snippet)
	}
	for _, p := range m.all {
		if p.Snippet != "" {
			t.Fatalf("search mutated all: %s snippet=%q", p.Name, p.Snippet)
		}
	}
	if v := m.View(); !strings.Contains(v, "fringe catches") || !strings.Contains(v, "1 of 3") {
		t.Fatalf("list view missing snippet or count:\n%s", v)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != planScreen {
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
