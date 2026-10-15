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
	defaultDesc = map[string]bool{"updated": true, "created": true, "size": true, "messages": true}

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
	pagerScreen
)

type listMode int

const (
	plansMode listMode = iota
	sessionsMode
)

type listKind struct {
	noun        string
	placeholder string
	sortKeys    []string
}

var listKinds = [...]listKind{
	plansMode:    {"plans", "Search titles and content", []string{"updated", "created", "title", "project", "size"}},
	sessionsMode: {"sessions", "Search session titles and conversations", []string{"updated", "created", "title", "project", "messages"}},
}

type sortState struct {
	key  int
	desc bool
}

type listItem interface {
	itemKey() string
	itemTitle() string
	itemMeta(q query, now time.Time) []string
	itemSnippet() string
}

type loadedMsg struct {
	plans    []Plan
	sessions []Session
	err      error
}

type model struct {
	store         *Store
	index         *TranscriptIndex
	plans         []Plan
	sessions      []Session
	shown         []listItem
	mode          listMode
	sorts         [len(listKinds)]sortState
	loaded        bool
	err           error
	status        string
	input         textinput.Model
	q             query
	cursor        int
	offset        int
	width, height int
	screen        screen
	pager         pager
	openResume    string
	resume        string
}

func runProgram(store *Store, index *TranscriptIndex) error {
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
	final, err := tea.NewProgram(newModel(store, index, style), tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	if m, ok := final.(model); ok && m.resume != "" {
		fmt.Println(m.resume)
	}
	return nil
}

func newModel(store *Store, index *TranscriptIndex, style string) model {
	in := textinput.New()
	in.Prompt = "> "
	in.PromptStyle = barStyle
	in.Placeholder = listKinds[plansMode].placeholder
	in.Focus()
	m := model{
		store: store,
		index: index,
		input: in,
		pager: newPager(style),
	}
	for i := range m.sorts {
		m.sorts[i].desc = true
	}
	return m
}

func (m model) kind() listKind {
	return listKinds[m.mode]
}

func (m model) total() int {
	if m.mode == sessionsMode {
		return len(m.sessions)
	}
	return len(m.plans)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.load(), textinput.Blink)
}

func (m model) load() tea.Cmd {
	store, index := m.store, m.index
	return func() tea.Msg {
		plans, err := store.List()
		var sessions []Session
		if index != nil {
			index.Refresh()
			sessions = index.Sessions()
		}
		return loadedMsg{plans, sessions, err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.pager.setSize(msg.Width, msg.Height)
		m.fixOffset()
		return m, nil
	case loadedMsg:
		m.loaded = true
		m.err = msg.err
		name := m.selectedName()
		if msg.err == nil {
			m.plans = msg.plans
		}
		m.sessions = msg.sessions
		m.refilter()
		m.reselect(name)
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	if m.screen == pagerScreen {
		return m.updatePager(msg)
	}
	return m.updateList(msg)
}

func (m model) updatePager(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "r" && m.openResume != "" && m.pager.idle() {
		m.resume = m.openResume
		return m, tea.Quit
	}
	var cmd tea.Cmd
	var back bool
	m.pager, cmd, back = m.pager.Update(msg)
	if back {
		m.screen = listScreen
		m.openResume = ""
		return m, m.load()
	}
	return m, cmd
}

func (m model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
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
			st := &m.sorts[m.mode]
			st.key = (st.key + 1) % len(m.kind().sortKeys)
			st.desc = defaultDesc[m.kind().sortKeys[st.key]]
			m.resort()
			return m, nil
		case "shift+tab":
			m.sorts[m.mode].desc = !m.sorts[m.mode].desc
			m.resort()
			return m, nil
		case "ctrl+t":
			m.toggleMode()
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
	key := m.selectedName()
	if key == "" {
		return m, nil
	}
	doc, resume, err := m.document(key)
	if err != nil {
		m.status = fmt.Sprintf("cannot open %s: %v", key, err)
		return m, m.load()
	}
	m.pager.open(doc, m.q.terms)
	m.openResume = resume
	m.screen = pagerScreen
	return m, nil
}

func (m model) document(key string) (document, string, error) {
	now := time.Now()
	if m.mode == sessionsMode {
		m.index.Refresh()
		s, ok := m.index.Session(key)
		if !ok {
			return document{}, "", errSessionNotFound
		}
		return sessionDocument(s, now), s.resumeCommand(), nil
	}
	p, err := m.store.Get(key)
	if err != nil {
		return document{}, "", err
	}
	return planDocument(p, now), "", nil
}

func planDocument(p Plan, now time.Time) document {
	meta := []string{p.Name}
	if p.Project != "" {
		meta = append(meta, p.Project)
	}
	meta = append(meta, "created "+p.createdLabel(now), "updated "+relativeTime(p.Updated, now))
	return document{name: p.Name, title: p.Title, meta: meta, body: p.body, updated: p.Updated}
}

func sessionDocument(s Session, now time.Time) document {
	var meta []string
	for _, f := range []string{s.Project, s.Branch} {
		if f != "" {
			meta = append(meta, f)
		}
	}
	meta = append(meta,
		"created "+relativeTime(s.Created, now),
		"updated "+relativeTime(s.Updated, now),
		messageCount(s.Messages))
	return document{name: s.ID, title: s.Title, meta: meta, body: s.markdown(), turns: s.turns, updated: s.Updated, hint: "r resume"}
}

func (m *model) toggleMode() {
	if m.index == nil {
		m.status = "no transcripts directory, sessions unavailable"
		return
	}
	m.mode = (m.mode + 1) % listMode(len(listKinds))
	m.input.Placeholder = m.kind().placeholder
	m.cursor, m.offset = 0, 0
	m.refilter()
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
	st := m.sorts[m.mode]
	key := m.kind().sortKeys[st.key]
	switch m.mode {
	case sessionsMode:
		ss := filterSessions(slices.Clone(m.sessions), m.q)
		sortSessions(ss, key, st.desc)
		m.shown = listItems(ss)
	default:
		ps := filterPlans(slices.Clone(m.plans), m.q)
		sortPlans(ps, key, st.desc)
		m.shown = listItems(ps)
	}
	m.cursor = min(m.cursor, max(len(m.shown)-1, 0))
	m.fixOffset()
}

func listItems[T listItem](xs []T) []listItem {
	out := make([]listItem, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

func (m *model) reselect(name string) {
	for i := range m.shown {
		if m.shown[i].itemKey() == name {
			m.cursor = i
			m.fixOffset()
			return
		}
	}
}

func (m model) selectedName() string {
	if m.cursor < len(m.shown) {
		return m.shown[m.cursor].itemKey()
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

func itemHeight(it listItem) int {
	if it.itemSnippet() != "" {
		return 4
	}
	return 3
}

func (m *model) fixOffset() {
	m.offset = min(m.offset, m.cursor)
	for m.offset < m.cursor {
		h := 0
		for i := m.offset; i <= m.cursor; i++ {
			h += itemHeight(m.shown[i])
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
	if m.screen == pagerScreen {
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
	count := fmt.Sprintf("%d %s", m.total(), m.kind().noun)
	if len(m.shown) != m.total() {
		count = fmt.Sprintf("%d of %d %s", len(m.shown), m.total(), m.kind().noun)
	}
	st := m.sorts[m.mode]
	arrow := "↑"
	if st.desc {
		arrow = "↓"
	}
	right := mutedStyle.Render(fmt.Sprintf("%s · %s %s", count, m.kind().sortKeys[st.key], arrow))
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
	toggle := ""
	if m.index != nil {
		toggle = " · ctrl+t " + listKinds[(m.mode+1)%listMode(len(listKinds))].noun
	}
	return mutedStyle.Render("↑/↓ move · enter open · tab sort · shift+tab reverse" + toggle + " · ctrl+r reload · esc clear/quit")
}

func (m model) bodyLines() []string {
	switch {
	case m.err != nil && m.mode == plansMode:
		return []string{errorStyle.Render("Cannot read plans: " + m.err.Error())}
	case !m.loaded:
		return []string{mutedStyle.Render("Loading " + m.kind().noun + "…")}
	case len(m.shown) == 0:
		return []string{mutedStyle.Render("No " + m.kind().noun + " match.")}
	}
	var lines []string
	now := time.Now()
	for i := m.offset; i < len(m.shown) && len(lines) < m.listHeight(); i++ {
		lines = append(lines, m.itemLines(m.shown[i], i == m.cursor, now)...)
	}
	return lines
}

func (m model) itemLines(it listItem, selected bool, now time.Time) []string {
	bar, title := "  ", titleStyle
	if selected {
		bar, title = barStyle.Render("▌ "), selectedStyle
	}
	lines := []string{
		bar + m.q.highlight(it.itemTitle(), title),
		bar + strings.Join(it.itemMeta(m.q, now), mutedStyle.Render(" · ")),
	}
	if s := it.itemSnippet(); s != "" {
		lines = append(lines, bar+m.q.highlight(s, plainStyle))
	}
	return append(lines, "")
}

func (p Plan) itemKey() string     { return p.Name }
func (p Plan) itemTitle() string   { return p.Title }
func (p Plan) itemSnippet() string { return p.Snippet }

func (p Plan) itemMeta(q query, now time.Time) []string {
	meta := []string{q.highlight(p.Name, mutedStyle)}
	if p.Project != "" {
		meta = append(meta, q.highlight(p.Project, mutedStyle))
	}
	return append(meta,
		mutedStyle.Render("created "+p.createdLabel(now)),
		mutedStyle.Render("updated "+relativeTime(p.Updated, now)),
		mutedStyle.Render(formatSize(p.Size)))
}

func (p Plan) createdLabel(now time.Time) string {
	if p.CreatedExact {
		return relativeTime(p.Created, now)
	}
	return "~" + relativeTime(p.Created, now)
}

func (s Session) itemKey() string     { return s.ID }
func (s Session) itemTitle() string   { return s.Title }
func (s Session) itemSnippet() string { return s.Snippet }

func (s Session) itemMeta(q query, now time.Time) []string {
	var meta []string
	for _, f := range []string{s.Project, s.Branch} {
		if f != "" {
			meta = append(meta, q.highlight(f, mutedStyle))
		}
	}
	return append(meta,
		mutedStyle.Render("created "+relativeTime(s.Created, now)),
		mutedStyle.Render("updated "+relativeTime(s.Updated, now)),
		mutedStyle.Render(messageCount(s.Messages)))
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

type sortFields struct {
	title, project   string
	created, updated time.Time
	count            int64
}

func compareFields(a, b sortFields, key string, desc bool) int {
	var r int
	switch key {
	case "title":
		r = strings.Compare(strings.ToLower(a.title), strings.ToLower(b.title))
	case "project":
		r = strings.Compare(strings.ToLower(a.project), strings.ToLower(b.project))
	case "size", "messages":
		r = cmp.Compare(a.count, b.count)
	case "created":
		r = a.created.Compare(b.created)
	default:
		r = a.updated.Compare(b.updated)
	}
	if desc {
		r = -r
	}
	if r != 0 {
		return r
	}
	return b.updated.Compare(a.updated)
}

func sortPlans(plans []Plan, key string, desc bool) {
	fields := func(p Plan) sortFields { return sortFields{p.Title, p.Project, p.Created, p.Updated, p.Size} }
	slices.SortStableFunc(plans, func(a, b Plan) int { return compareFields(fields(a), fields(b), key, desc) })
}

func sortSessions(sessions []Session, key string, desc bool) {
	fields := func(s Session) sortFields {
		return sortFields{s.Title, s.Project, s.Created, s.Updated, int64(s.Messages)}
	}
	slices.SortStableFunc(sessions, func(a, b Session) int { return compareFields(fields(a), fields(b), key, desc) })
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

func messageCount(n int) string {
	if n == 1 {
		return "1 message"
	}
	return fmt.Sprintf("%d messages", n)
}

func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}
