package main

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	maxWrap          = 120
	pagerHeaderLines = 2
	pagerFooterLines = 1
	matchContext     = 2
	wheelLines       = 3
)

var (
	statusBarStyle = lipgloss.NewStyle().Reverse(true)
	currentMark    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Bold(true)
	pagerHelp      = [][2]string{
		{"↓ j enter ctrl+n", "down one line"},
		{"↑ k ctrl+p", "up one line"},
		{"space d f pgdn", "down one page"},
		{"u delete pgup", "up one page"},
		{"t g home", "top"},
		{"b G end", "bottom"},
		{"/ s", "search forward"},
		{"?", "search backward"},
		{"n / N", "next match / previous match"},
		{"%", "go to percent"},
		{"J", "go to line"},
		{"h", "this help"},
		{"q esc", "back to the list"},
		{"ctrl+c", "quit"},
	}
)

type promptKind int

const (
	noPrompt promptKind = iota
	searchForward
	searchBackward
	gotoPercent
	gotoLine
)

var promptLabels = map[promptKind]string{
	searchForward:  "/",
	searchBackward: "?",
	gotoPercent:    "Percent: ",
	gotoLine:       "Line: ",
}

type renderKey struct {
	name    string
	width   int
	updated time.Time
}

type match struct {
	line, start, end int
}

type pager struct {
	style    string
	plan     Plan
	width    int
	height   int
	rendered renderKey
	err      error
	lines    []string
	top      int
	terms    []*regexp.Regexp
	backward bool
	matches  []match
	cur      int
	prompt   promptKind
	input    textinput.Model
	status   string
	help     bool
}

func newPager(style string) pager {
	in := textinput.New()
	return pager{style: style, input: in, cur: -1, height: 1}
}

func (p *pager) setSize(w, h int) {
	p.width = w
	p.height = max(1, h-pagerHeaderLines-pagerFooterLines)
	p.input.Width = max(1, w-len(p.input.Prompt)-1)
	if p.plan.Name != "" {
		p.render()
	}
}

func (p *pager) open(plan Plan, terms []*regexp.Regexp) {
	p.plan, p.terms = plan, terms
	p.backward, p.help, p.prompt, p.status = false, false, noPrompt, ""
	p.render()
	p.findMatches()
	p.top, p.cur = 0, -1
	if len(p.matches) > 0 {
		p.cur = 0
		p.showMatch()
	}
}

func (p *pager) render() {
	wrap := min(p.width, maxWrap)
	key := renderKey{p.plan.Name, wrap, p.plan.Updated}
	if key == p.rendered {
		return
	}
	sameDoc := key.name == p.rendered.name && len(p.lines) > 0
	oldLen := len(p.lines)
	p.rendered = key
	out, err := p.renderMarkdown(wrap)
	p.err = err
	if err != nil {
		out = string(p.plan.body)
	}
	p.lines = trimBlank(strings.Split(out, "\n"))
	if sameDoc {
		p.top = p.top * len(p.lines) / oldLen
	}
	p.findMatches()
	p.scrollTo(p.top)
}

func (p *pager) renderMarkdown(wrap int) (string, error) {
	r, err := glamour.NewTermRenderer(glamour.WithStylePath(p.style), glamour.WithWordWrap(max(20, wrap-4)))
	if err != nil {
		return "", err
	}
	return r.Render(string(p.plan.body))
}

func trimBlank(lines []string) []string {
	blank := func(s string) bool { return strings.TrimSpace(ansi.Strip(s)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (p *pager) findMatches() {
	p.matches = nil
	for i, line := range p.lines {
		plain := ansi.Strip(line)
		var found []match
		for _, re := range p.terms {
			for _, loc := range re.FindAllStringIndex(plain, -1) {
				if loc[0] < loc[1] {
					found = append(found, match{i, ansi.StringWidth(plain[:loc[0]]), ansi.StringWidth(plain[:loc[1]])})
				}
			}
		}
		slices.SortFunc(found, func(a, b match) int { return a.start - b.start })
		end := -1
		for _, m := range found {
			if m.start >= end {
				p.matches = append(p.matches, m)
				end = m.end
			}
		}
	}
	p.cur = min(p.cur, len(p.matches)-1)
}

func (p *pager) maxTop() int {
	return max(0, len(p.lines)-p.height)
}

func (p *pager) scrollTo(n int) {
	p.top = max(0, min(n, p.maxTop()))
}

func (p *pager) visible(line int) bool {
	return line >= p.top && line < p.top+p.height
}

func (p *pager) showMatch() {
	if line := p.matches[p.cur].line; !p.visible(line) {
		p.scrollTo(line - matchContext)
	}
}

func (p *pager) nextMatch(backward bool) (int, bool) {
	n := len(p.matches)
	if p.cur >= 0 && p.visible(p.matches[p.cur].line) {
		if backward {
			return (p.cur - 1 + n) % n, p.cur == 0
		}
		return (p.cur + 1) % n, p.cur == n-1
	}
	if backward {
		for i := n - 1; i >= 0; i-- {
			if p.matches[i].line < p.top {
				return i, false
			}
		}
		return n - 1, true
	}
	for i, m := range p.matches {
		if m.line >= p.top {
			return i, false
		}
	}
	return 0, true
}

func (p *pager) step(backward bool) {
	switch {
	case len(p.terms) == 0:
		p.status = "No previous search"
	case len(p.matches) == 0:
		p.status = "Pattern not found"
	default:
		next, wrapped := p.nextMatch(backward)
		p.cur = next
		if wrapped {
			p.status = "Search wrapped"
		}
		p.showMatch()
	}
}

func (p *pager) search(text string, backward bool) {
	p.backward = backward
	if text != "" {
		p.terms = []*regexp.Regexp{regexp.MustCompile(`(?i)` + regexp.QuoteMeta(text))}
		p.findMatches()
		p.cur = -1
	}
	p.step(backward)
}

func (p *pager) openPrompt(kind promptKind) tea.Cmd {
	p.prompt = kind
	p.input.Reset()
	p.input.Prompt = promptLabels[kind]
	p.input.Width = max(1, p.width-len(p.input.Prompt)-1)
	return p.input.Focus()
}

func (p *pager) submitPrompt() {
	kind, text := p.prompt, strings.TrimSpace(p.input.Value())
	p.prompt = noPrompt
	p.input.Blur()
	switch kind {
	case searchForward, searchBackward:
		p.search(text, kind == searchBackward)
		return
	}
	if text == "" {
		return
	}
	n, err := strconv.Atoi(strings.TrimSuffix(text, "%"))
	if err != nil {
		p.status = "Not a number: " + text
		return
	}
	if kind == gotoPercent {
		p.scrollTo(len(p.lines) * n / 100)
	} else {
		p.scrollTo(n - 1)
	}
}

func (p pager) Update(msg tea.Msg) (pager, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				p.scrollTo(p.top - wheelLines)
			case tea.MouseButtonWheelDown:
				p.scrollTo(p.top + wheelLines)
			}
		}
		return p, nil, false
	case tea.KeyMsg:
		return p.handleKey(msg)
	}
	if p.prompt != noPrompt {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return p, cmd, false
	}
	return p, nil, false
}

func (p pager) handleKey(k tea.KeyMsg) (pager, tea.Cmd, bool) {
	if k.Type == tea.KeyRunes && len(k.Runes) > 1 && !k.Paste && p.prompt == noPrompt {
		return p.handleRunes(k.Runes)
	}
	if p.prompt != noPrompt {
		switch k.String() {
		case "enter":
			p.submitPrompt()
			return p, nil, false
		case "esc":
			p.prompt = noPrompt
			p.input.Blur()
			return p, nil, false
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(k)
		return p, cmd, false
	}
	p.status = ""
	if p.help {
		p.help = false
		return p, nil, false
	}
	switch k.String() {
	case "down", "enter", "j", "ctrl+n", "ctrl+e":
		p.scrollTo(p.top + 1)
	case "up", "k", "K", "ctrl+p", "ctrl+y":
		p.scrollTo(p.top - 1)
	case " ", "d", "D", "f", "F", "pgdown", "ctrl+d", "ctrl+f":
		p.scrollTo(p.top + p.height)
	case "u", "U", "delete", "backspace", "pgup", "ctrl+u", "ctrl+b":
		p.scrollTo(p.top - p.height)
	case "t", "T", "g", "home":
		p.scrollTo(0)
	case "b", "B", "G", "end":
		p.scrollTo(p.maxTop())
	case "/", "s", "S":
		return p, p.openPrompt(searchForward), false
	case "?":
		return p, p.openPrompt(searchBackward), false
	case "n":
		p.step(p.backward)
	case "N":
		p.step(!p.backward)
	case "%":
		return p, p.openPrompt(gotoPercent), false
	case "J":
		return p, p.openPrompt(gotoLine), false
	case "h", "H":
		p.help = true
	case "q", "Q", "esc":
		return p, nil, true
	}
	return p, nil, false
}

func (p pager) handleRunes(runes []rune) (pager, tea.Cmd, bool) {
	var cmds []tea.Cmd
	for i, r := range runes {
		if p.prompt != noPrompt {
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: runes[i:]})
			return p, tea.Batch(append(cmds, cmd)...), false
		}
		var cmd tea.Cmd
		var back bool
		p, cmd, back = p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if back {
			return p, nil, true
		}
		cmds = append(cmds, cmd)
	}
	return p, tea.Batch(cmds...), false
}

func (p pager) View() string {
	out := append(p.headerLines(), p.bodyLines()...)
	return strings.Join(append(out, p.statusView()), "\n")
}

func (p pager) headerLines() []string {
	now := time.Now()
	meta := []string{p.plan.Name}
	if p.plan.Project != "" {
		meta = append(meta, p.plan.Project)
	}
	created := relativeTime(p.plan.Created, now)
	if !p.plan.CreatedExact {
		created = "~" + created
	}
	meta = append(meta, "created "+created, "updated "+relativeTime(p.plan.Updated, now))
	return []string{
		ansi.Truncate(selectedStyle.Render(p.plan.Title), p.width, "…"),
		ansi.Truncate(mutedStyle.Render(strings.Join(meta, " · ")), p.width, "…"),
	}
}

func (p pager) bodyLines() []string {
	var lines []string
	if p.help {
		lines = append(lines, titleStyle.Render("Keys"), "")
		for _, h := range pagerHelp {
			lines = append(lines, fmt.Sprintf("  %-20s %s", h[0], mutedStyle.Render(h[1])))
		}
		lines = append(lines, "", mutedStyle.Render("Press any key to close."))
	} else {
		j, _ := slices.BinarySearchFunc(p.matches, p.top, func(m match, line int) int { return m.line - line })
		for i := p.top; i < min(len(p.lines), p.top+p.height); i++ {
			k := j
			for k < len(p.matches) && p.matches[k].line == i {
				k++
			}
			cur := -1
			if p.cur >= j && p.cur < k {
				cur = p.cur - j
			}
			lines = append(lines, highlightLine(p.lines[i], p.matches[j:k], cur))
			j = k
		}
	}
	if len(lines) > p.height {
		lines = lines[:p.height]
	}
	for len(lines) < p.height {
		lines = append(lines, "")
	}
	return lines
}

func highlightLine(line string, ranges []match, current int) string {
	if len(ranges) == 0 {
		return line
	}
	var b strings.Builder
	col := 0
	for i, r := range ranges {
		b.WriteString(ansi.Cut(line, col, r.start))
		st := markStyle
		if i == current {
			st = currentMark
		}
		b.WriteString(st.Render(ansi.Strip(ansi.Cut(line, r.start, r.end))))
		col = r.end
	}
	b.WriteString(ansi.Cut(line, col, ansi.StringWidth(line)))
	return b.String()
}

func (p pager) percent() int {
	if len(p.lines) <= p.height {
		return 100
	}
	return min(len(p.lines), p.top+p.height) * 100 / len(p.lines)
}

func (p pager) statusView() string {
	if p.prompt != noPrompt {
		return ansi.Truncate(p.input.View(), p.width, "")
	}
	left := " " + p.plan.Name
	switch {
	case p.status != "":
		left = " " + p.status
	case p.err != nil:
		left = " render failed, showing raw: " + p.err.Error()
	}
	var right []string
	if len(p.matches) > 0 && p.cur >= 0 {
		right = append(right, fmt.Sprintf("match %d/%d", p.cur+1, len(p.matches)))
	}
	first := min(p.top+1, len(p.lines))
	last := min(p.top+p.height, len(p.lines))
	right = append(right,
		fmt.Sprintf("line %d-%d/%d", first, last, len(p.lines)),
		fmt.Sprintf("%d%%", p.percent()),
		"h help ")
	r := strings.Join(right, "  ")
	gap := max(1, p.width-ansi.StringWidth(left)-ansi.StringWidth(r))
	bar := ansi.Truncate(left+strings.Repeat(" ", gap)+r, p.width, "")
	return statusBarStyle.Render(bar)
}
