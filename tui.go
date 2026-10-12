package main

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	headerLines = 2
	footerLines = 1
)

var (
	sortKeys    = []string{"updated", "created", "title", "project", "size"}
	defaultDesc = map[string]bool{"updated": true, "created": true, "size": true}

	accent        = lipgloss.AdaptiveColor{Light: "#5A56E0", Dark: "#8B87FF"}
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#7A7A7A", Dark: "#8A8A8A"})
	plainStyle    = lipgloss.NewStyle()
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = titleStyle.Foreground(accent)
	barStyle      = lipgloss.NewStyle().Foreground(accent)
	markStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#1A1A1A")).Background(lipgloss.Color("#F5D76E"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#C0392B", Dark: "#FF6B6B"})
)

type screen int

const (
	listScreen screen = iota
	planScreen
)

type plansMsg struct {
	plans []Plan
	err   error
}

type model struct {
	store         *Store
	transcripts   bool
	all, shown    []Plan
	loaded        bool
	err           error
	status        string
	input         textinput.Model
	q             query
	cursor        int
	offset        int
	sortKey       int
	desc          bool
	width, height int
	screen        screen
	pager         pager
}

func runProgram(store *Store, transcripts bool) error {
	style := os.Getenv("GLAMOUR_STYLE")
	if style == "" {
		style = "light"
		if lipgloss.HasDarkBackground() {
			style = "dark"
		}
	}
	if _, err := glamour.NewTermRenderer(glamour.WithStylePath(style)); err != nil {
		return fmt.Errorf("markdown style %q: %w", style, err)
	}
	_, err := tea.NewProgram(newModel(store, transcripts, style), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func newModel(store *Store, transcripts bool, style string) model {
	in := textinput.New()
	in.Prompt = "> "
	in.PromptStyle = barStyle
	in.Placeholder = "Search titles and content"
	in.Focus()
	return model{
		store:       store,
		transcripts: transcripts,
		input:       in,
		desc:        true,
		pager:       newPager(style),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.load(), textinput.Blink)
}

func (m model) load() tea.Cmd {
	store := m.store
	return func() tea.Msg {
		plans, err := store.List()
		return plansMsg{plans, err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.pager.setSize(msg.Width, msg.Height)
		m.fixOffset()
		return m, nil
	case plansMsg:
		m.loaded = true
		m.err = msg.err
		if msg.err == nil {
			name := m.selectedName()
			m.all = msg.plans
			m.refilter()
			m.reselect(name)
		}
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	if m.screen == planScreen {
		return m.updatePlan(msg)
	}
	return m.updateList(msg)
}

func (m model) updatePlan(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var back bool
	m.pager, cmd, back = m.pager.Update(msg)
	if back {
		m.screen = listScreen
		return m, m.load()
	}
	return m, cmd
}

func (m model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.move(-1)
		case tea.MouseButtonWheelDown:
			m.move(1)
		}
		return m, nil
	case tea.KeyMsg:
		m.status = ""
		switch msg.String() {
		case "up", "ctrl+p":
			m.move(-1)
			return m, nil
		case "down", "ctrl+n":
			m.move(1)
			return m, nil
		case "pgup":
			m.move(-m.pageSize())
			return m, nil
		case "pgdown":
			m.move(m.pageSize())
			return m, nil
		case "enter":
			return m.open()
		case "tab":
			m.sortKey = (m.sortKey + 1) % len(sortKeys)
			m.desc = defaultDesc[sortKeys[m.sortKey]]
			m.resort()
			return m, nil
		case "shift+tab":
			m.desc = !m.desc
			m.resort()
			return m, nil
		case "ctrl+r":
			return m, m.load()
		case "esc":
			if m.input.Value() == "" {
				return m, tea.Quit
			}
			m.input.SetValue("")
			m.setQuery()
			return m, nil
		}
	}
	prev := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != prev {
		m.setQuery()
	}
	return m, cmd
}

func (m model) open() (tea.Model, tea.Cmd) {
	name := m.selectedName()
	if name == "" {
		return m, nil
	}
	p, err := m.store.Get(name)
	if err != nil {
		m.status = fmt.Sprintf("cannot open %s: %v", name, err)
		return m, m.load()
	}
	m.pager.open(p, m.q.terms)
	m.screen = planScreen
	return m, nil
}

func (m *model) setQuery() {
	m.q = parseQuery(m.input.Value())
	m.cursor, m.offset = 0, 0
	m.refilter()
}

func (m *model) resort() {
	name := m.selectedName()
	m.refilter()
	m.reselect(name)
}

func (m *model) refilter() {
	m.shown = filterPlans(slices.Clone(m.all), m.q)
	sortPlans(m.shown, sortKeys[m.sortKey], m.desc)
	m.cursor = min(m.cursor, max(len(m.shown)-1, 0))
	m.fixOffset()
}

func (m *model) reselect(name string) {
	for i := range m.shown {
		if m.shown[i].Name == name {
			m.cursor = i
			m.fixOffset()
			return
		}
	}
}

func (m model) selectedName() string {
	if m.cursor < len(m.shown) {
		return m.shown[m.cursor].Name
	}
	return ""
}

func (m *model) move(delta int) {
	if len(m.shown) == 0 {
		return
	}
	m.cursor = max(0, min(len(m.shown)-1, m.cursor+delta))
	m.fixOffset()
}

func (m model) listHeight() int {
	return max(1, m.height-headerLines-footerLines)
}

func (m model) pageSize() int {
	return max(1, m.listHeight()/3)
}

func itemHeight(p *Plan) int {
	if p.Snippet != "" {
		return 4
	}
	return 3
}

func (m *model) fixOffset() {
	m.offset = min(m.offset, m.cursor)
	for m.offset < m.cursor {
		h := 0
		for i := m.offset; i <= m.cursor; i++ {
			h += itemHeight(&m.shown[i])
		}
		if h <= m.listHeight()+1 {
			return
		}
		m.offset++
	}
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.screen == planScreen {
		return m.pager.View()
	}
	return m.listView()
}

func (m model) listView() string {
	lines := m.bodyLines()
	avail := m.listHeight()
	if len(lines) > avail {
		lines = lines[:avail]
	}
	for len(lines) < avail {
		lines = append(lines, "")
	}
	out := append([]string{m.headerView(), ""}, lines...)
	out = append(out, m.footerView())
	for i, l := range out {
		out[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(out, "\n")
}

func (m model) headerView() string {
	count := fmt.Sprintf("%d plans", len(m.all))
	if len(m.shown) != len(m.all) {
		count = fmt.Sprintf("%d of %d", len(m.shown), len(m.all))
	}
	arrow := "↑"
	if m.desc {
		arrow = "↓"
	}
	right := mutedStyle.Render(fmt.Sprintf("%s · %s %s", count, sortKeys[m.sortKey], arrow))
	in := m.input
	in.Width = max(10, m.width-lipgloss.Width(right)-len(in.Prompt)-2)
	left := in.View()
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func (m model) footerView() string {
	if m.status != "" {
		return errorStyle.Render(m.status)
	}
	return mutedStyle.Render("↑/↓ move · enter open · tab sort · shift+tab reverse · ctrl+r reload · esc clear/quit")
}

func (m model) bodyLines() []string {
	switch {
	case m.err != nil:
		return []string{errorStyle.Render("Cannot read plans: " + m.err.Error())}
	case !m.loaded:
		return []string{mutedStyle.Render("Loading plans…")}
	case len(m.shown) == 0:
		return []string{mutedStyle.Render("No plans match.")}
	}
	var lines []string
	now := time.Now()
	for i := m.offset; i < len(m.shown) && len(lines) < m.listHeight(); i++ {
		lines = append(lines, m.itemLines(&m.shown[i], i == m.cursor, now)...)
	}
	return lines
}

func (m model) itemLines(p *Plan, selected bool, now time.Time) []string {
	bar, title := "  ", titleStyle
	if selected {
		bar, title = barStyle.Render("▌ "), selectedStyle
	}
	meta := []string{m.q.highlight(p.Name, mutedStyle)}
	if m.transcripts && p.Project != "" {
		meta = append(meta, m.q.highlight(p.Project, mutedStyle))
	}
	created := relativeTime(p.Created, now)
	if !p.CreatedExact {
		created = "~" + created
	}
	meta = append(meta,
		mutedStyle.Render("created "+created),
		mutedStyle.Render("updated "+relativeTime(p.Updated, now)),
		mutedStyle.Render(formatSize(p.Size)))
	lines := []string{
		bar + m.q.highlight(p.Title, title),
		bar + strings.Join(meta, mutedStyle.Render(" · ")),
	}
	if p.Snippet != "" {
		lines = append(lines, bar+m.q.highlight(p.Snippet, plainStyle))
	}
	return append(lines, "")
}

func (q query) highlight(s string, base lipgloss.Style) string {
	marked := make([]bool, len(s))
	for _, re := range q.terms {
		for _, loc := range re.FindAllStringIndex(s, -1) {
			for i := loc[0]; i < loc[1]; i++ {
				marked[i] = true
			}
		}
	}
	mark := markStyle.Inherit(base)
	var b strings.Builder
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && marked[j] == marked[i] {
			j++
		}
		if marked[i] {
			b.WriteString(mark.Render(s[i:j]))
		} else {
			b.WriteString(base.Render(s[i:j]))
		}
		i = j
	}
	return b.String()
}

func sortPlans(plans []Plan, key string, desc bool) {
	slices.SortStableFunc(plans, func(a, b Plan) int {
		var r int
		switch key {
		case "title":
			r = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		case "project":
			r = strings.Compare(strings.ToLower(a.Project), strings.ToLower(b.Project))
		case "size":
			r = cmp.Compare(a.Size, b.Size)
		case "created":
			r = a.Created.Compare(b.Created)
		default:
			r = a.Updated.Compare(b.Updated)
		}
		if desc {
			r = -r
		}
		if r != 0 {
			return r
		}
		return b.Updated.Compare(a.Updated)
	})
}

func relativeTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	t = t.In(now.Location())
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}
