package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

var specialKeys = map[string]tea.KeyType{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEsc,
	"space":     tea.KeySpace,
	"backspace": tea.KeyBackspace,
	"pgdown":    tea.KeyPgDown,
	"pgup":      tea.KeyPgUp,
}

func keyMsg(k string) tea.KeyMsg {
	if t, ok := specialKeys[k]; ok {
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func press(t *testing.T, p pager, keys ...string) pager {
	t.Helper()
	for _, k := range keys {
		var back bool
		p, _, back = p.Update(keyMsg(k))
		if back {
			t.Fatalf("key %q left the plan", k)
		}
	}
	return p
}

func repeat(k string, n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = k
	}
	return keys
}

func testPager(t *testing.T, items int, terms string) pager {
	t.Helper()
	var b strings.Builder
	b.WriteString("# Title\n\n")
	for i := 1; i <= items; i++ {
		fmt.Fprintf(&b, "- item %d\n", i)
	}
	b.WriteString("\n- needle at the end\n")
	p := newPager("notty")
	p.setSize(80, 20)
	p.open(document{name: "a.md", title: "Title", body: []byte(b.String())}, parseQuery(terms).terms)
	return p
}

func TestPagerScrollClamps(t *testing.T) {
	p := testPager(t, 100, "")
	if strings.TrimSpace(ansi.Strip(p.lines[len(p.lines)-1])) == "" || strings.TrimSpace(ansi.Strip(p.lines[0])) == "" {
		t.Fatal("rendered output keeps leading or trailing blank lines")
	}
	bottom := len(p.lines) - p.height

	for _, k := range []string{"j", "space", "d", "enter", "pgdown", "b", "G"} {
		p = press(t, p, repeat(k, 200)...)
		if p.top != bottom {
			t.Fatalf("%q past the end: top=%d, want %d", k, p.top, bottom)
		}
	}
	if p.percent() != 100 {
		t.Fatalf("percent at bottom = %d", p.percent())
	}

	p = press(t, p, repeat("k", 200)...)
	if p.top != 0 {
		t.Fatalf("k past the top: top=%d", p.top)
	}
	p = press(t, p, "b", "t")
	if p.top != 0 {
		t.Fatalf("t: top=%d", p.top)
	}
	p = press(t, p, "u", "backspace", "k", "pgup")
	if p.top != 0 {
		t.Fatalf("past the top: top=%d", p.top)
	}

	short := testPager(t, 3, "")
	for _, k := range []string{"space", "b", "j", "G"} {
		if short = press(t, short, k); short.top != 0 {
			t.Fatalf("short plan %q: top=%d", k, short.top)
		}
	}
}

func TestPagerSearch(t *testing.T) {
	p := testPager(t, 100, "")
	p = press(t, p, "/", "needle", "enter")
	if p.cur != 0 || len(p.matches) != 1 || !p.visible(p.matches[0].line) {
		t.Fatalf("search needle: cur=%d matches=%v top=%d", p.cur, p.matches, p.top)
	}

	p = press(t, p, "t", "/", "item 5", "enter")
	first := p.matches[p.cur].line
	if got := ansi.Strip(p.lines[first]); !strings.Contains(got, "item 5") {
		t.Fatalf("first match line = %q", got)
	}
	p = press(t, p, "n")
	if p.cur != 1 {
		t.Fatalf("n: cur=%d", p.cur)
	}
	p = press(t, p, "N", "N")
	if p.cur != len(p.matches)-1 || p.status != "Search wrapped" {
		t.Fatalf("N past the first: cur=%d status=%q", p.cur, p.status)
	}

	p = press(t, p, "b", "?", "item 1", "enter")
	if !strings.Contains(ansi.Strip(p.lines[p.matches[p.cur].line]), "item 1") || !p.visible(p.matches[p.cur].line) {
		t.Fatalf("backward search: cur=%d", p.cur)
	}

	top := p.top
	p = press(t, p, "/", "zzz", "enter")
	if p.status != "Pattern not found" || p.top != top {
		t.Fatalf("missing pattern: status=%q top=%d want %d", p.status, p.top, top)
	}

	p = press(t, p, "/", "abc", "esc")
	if p.prompt != noPrompt {
		t.Fatal("esc did not close the prompt")
	}
	if _, _, back := p.Update(keyMsg("esc")); !back {
		t.Fatal("esc without a prompt did not go back")
	}

	p = press(t, p, "t/needle")
	p = press(t, p, "enter")
	if p.cur != 0 || !p.visible(p.matches[0].line) {
		t.Fatal("burst of runes did not run the search")
	}

	q := testPager(t, 100, "needle")
	if q.cur != 0 || !q.visible(q.matches[0].line) {
		t.Fatalf("list query: cur=%d top=%d", q.cur, q.top)
	}
}

func TestPagerGoto(t *testing.T) {
	p := testPager(t, 100, "")
	p = press(t, p, "%", "50", "enter")
	if want := len(p.lines) / 2; p.top != want {
		t.Fatalf("50%%: top=%d, want %d", p.top, want)
	}
	p = press(t, p, "J", "10", "enter")
	if p.top != 9 {
		t.Fatalf("line 10: top=%d", p.top)
	}
	p = press(t, p, "J", "99999", "enter")
	if p.top != len(p.lines)-p.height {
		t.Fatalf("line past the end: top=%d", p.top)
	}
	p = press(t, p, "%", "abc", "enter")
	if !strings.HasPrefix(p.status, "Not a number") {
		t.Fatalf("status = %q", p.status)
	}
}

func TestHighlightLinePreservesText(t *testing.T) {
	line := markStyle.Render("ab") + "cd" + titleStyle.Render("éfgh") + "ij"
	plain := ansi.Strip(line)
	for _, ranges := range [][]match{
		{{0, 0, 2}},
		{{0, 3, 5}},
		{{0, 8, 10}},
		{{0, 0, 1}, {0, 4, 6}, {0, 9, 10}},
	} {
		if got := ansi.Strip(highlightLine(line, ranges, 0)); got != plain {
			t.Errorf("highlight %v: %q, want %q", ranges, got, plain)
		}
	}
}

func TestExpandTabs(t *testing.T) {
	for in, want := range map[string]string{
		"no tabs":                       "no tabs",
		"\tmu":                          "        mu",
		"  \tmu":                        "        mu",
		"a\tb\tc":                       "a       b       c",
		"éé\tx":                         "éé      x",
		"12345678\tx":                   "12345678        x",
		titleStyle.Render("ab") + "\tx": titleStyle.Render("ab") + "      x",
	} {
		if got := expandTabs(in); got != want {
			t.Errorf("expandTabs(%q) = %q, want %q", in, got, want)
		}
	}

	p := newPager("notty")
	p.setSize(80, 20)
	p.open(document{name: "a.md", body: []byte("```go\nfunc f() {\n\treturn needle\n}\n```\n")}, parseQuery("needle").terms)
	for _, l := range p.lines {
		if strings.Contains(l, "\t") {
			t.Fatalf("rendered line keeps a tab: %q", l)
		}
	}
	m := p.matches[0]
	if got := ansi.Strip(ansi.Cut(p.lines[m.line], m.start, m.end)); got != "needle" {
		t.Fatalf("match after a tab covers %q", got)
	}
}

func TestRenderConversation(t *testing.T) {
	md, err := glamour.NewTermRenderer(glamour.WithStylePath("dark"), glamour.WithWordWrap(40))
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderConversation(md, []turn{
		{user: true, text: "why does the vacuum stall whenever it reaches the persian rug?"},
		{text: "The fringe tangles around the brushroll.\n\n- lower the suction\n- lift the brush"},
		{user: true, text: "thanks"},
	}, 40)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(ansi.Strip(out), "\n")
	var trimmed []string
	for _, l := range lines {
		if ansi.StringWidth(l) > 40 {
			t.Errorf("line wider than 40: %q", l)
		}
		trimmed = append(trimmed, strings.TrimRight(l, " "))
	}
	want := []string{
		"> why does the vacuum stall whenever it",
		"  reaches the persian rug?",
		"",
		replyBullet + " The fringe tangles around the",
		"  brushroll.",
		"",
		"  • lower the suction",
		"  • lift the brush",
		"",
		"> thanks",
	}
	if strings.Join(trimmed, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(trimmed, "\n"), strings.Join(want, "\n"))
	}
}

func TestBulletFor(t *testing.T) {
	if bulletFor("darwin") != "⏺" || bulletFor("linux") != "●" {
		t.Fatalf("bulletFor = %q, %q", bulletFor("darwin"), bulletFor("linux"))
	}
}
