package main

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
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
	freshStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"})
	emphasisStyle = lipgloss.NewStyle().Bold(true)
	rowStyle      = lipgloss.NewStyle().Background(panelBg)

	sourceStyles = map[string]lipgloss.Style{
		sourceClaude: lipgloss.NewStyle().Foreground(lipgloss.Color("#1A1A1A")).Background(lipgloss.Color("#D97757")),
		sourceCodex:  lipgloss.NewStyle().Foreground(lipgloss.Color("#1A1A1A")).Background(lipgloss.Color("#10A37F")),
		sourceAgy:    lipgloss.NewStyle().Foreground(lipgloss.Color("#1A1A1A")).Background(lipgloss.Color("#8AB4F8")),
	}

	statusStyles = map[planStatus]lipgloss.Style{
		statusDraft:    mutedStyle,
		statusPending:  lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}),
		statusRejected: errorStyle,
		statusApproved: barStyle,
		statusWorked:   freshStyle,
	}

	projectPalette = [...]lipgloss.AdaptiveColor{
		{Light: "#0969DA", Dark: "#58A6FF"},
		{Light: "#1A7F37", Dark: "#3FB950"},
		{Light: "#BC4C00", Dark: "#F0883E"},
		{Light: "#8250DF", Dark: "#BC8CFF"},
		{Light: "#BF3989", Dark: "#F778BA"},
		{Light: "#1B7C83", Dark: "#39C5CF"},
		{Light: "#9A6700", Dark: "#D29922"},
		{Light: "#C4432B", Dark: "#F78166"},
	}
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
	itemSource() string
	itemTitle() string
	itemUpdated() time.Time
	itemMeta(q query, sortKey string, now time.Time) (left, right []string)
	itemSnippet() string
}

type loadedMsg struct {
	plans    []Plan
	sessions []Session
	err      error
}

type model struct {
	catalog       *Catalog
	labelSources  bool
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
	resume        string
}

func runProgram(catalog *Catalog) error {
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
	final, err := tea.NewProgram(newModel(catalog, style), tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	if m, ok := final.(model); ok && m.resume != "" {
		fmt.Println(m.resume)
	}
	return nil
}

func newModel(catalog *Catalog, style string) model {
	in := textinput.New()
	in.Prompt = "> "
	in.PromptStyle = barStyle
	in.Placeholder = listKinds[plansMode].placeholder
	in.Cursor.SetMode(cursor.CursorStatic)
	in.Focus()
	m := model{
		catalog:      catalog,
		labelSources: len(catalog.Sources()) > 1,
		input:        in,
		pager:        newPager(style),
	}
	for i := range m.sorts {
		m.sorts[i].desc = true
	}
	return m
}

func (m model) kind() listKind {
	return listKinds[m.mode]
}

func (m model) sortKey() string {
	return m.kind().sortKeys[m.sorts[m.mode].key]
}

func (m model) total() int {
	if m.mode == sessionsMode {
		return len(m.sessions)
	}
	return len(m.plans)
}

func (m model) Init() tea.Cmd {
	return m.load()
}

func (m model) load() tea.Cmd {
	c := m.catalog
	return func() tea.Msg {
		plans, err := c.Plans()
		return loadedMsg{plans, c.Sessions(), err}
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
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "r" && m.pager.doc.resume != "" && m.pager.idle() {
		m.resume = m.pager.doc.resume
		return m, tea.Quit
	}
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
	doc, err := m.document(key)
	if err != nil {
		m.status = fmt.Sprintf("cannot open %s: %v", key, err)
		return m, m.load()
	}
	m.pager.open(doc, m.q.terms)
	m.screen = pagerScreen
	return m, nil
}

func (m model) document(key string) (document, error) {
	now := time.Now()
	var doc document
	var source string
	if m.mode == sessionsMode {
		s, ok := m.catalog.Session(key)
		if !ok {
			return document{}, errSessionNotFound
		}
		home, _ := os.UserHomeDir()
		doc, source = sessionDocument(s, now, home), s.Source
	} else {
		p, err := m.catalog.Plan(key)
		if err != nil {
			return document{}, err
		}
		doc, source = planDocument(p, now), p.Source
	}
	if m.labelSources {
		doc.meta = append([]string{source}, doc.meta...)
	}
	return doc, nil
}

func planDocument(p Plan, now time.Time) document {
	meta := []string{p.Name}
	if p.Project != "" {
		meta = append(meta, p.Project)
	}
	if p.Status != "" {
		meta = append(meta, string(p.Status))
	}
	meta = append(meta, "created "+p.createdLabel(now), "updated "+relativeTime(p.Updated, now))
	return document{name: p.Name, title: p.Title, meta: meta, body: p.body, updated: p.Updated}
}

func sessionDocument(s Session, now time.Time, home string) document {
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
	return document{
		name:    s.ID,
		title:   s.Title,
		meta:    meta,
		body:    s.markdown(),
		turns:   s.turns,
		updated: s.Updated,
		resume:  s.resumeCommand(home),
	}
}

func (m *model) toggleMode() {
	if !m.catalog.HasSessions() {
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
	key, desc := m.sortKey(), m.sorts[m.mode].desc
	switch m.mode {
	case sessionsMode:
		ss := filterSessions(slices.Clone(m.sessions), m.q)
		sortSessions(ss, key, desc)
		m.shown = listItems(ss)
	default:
		ps := filterPlans(slices.Clone(m.plans), m.q)
		sortPlans(ps, key, desc)
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
	arrow := "↑"
	if m.sorts[m.mode].desc {
		arrow = "↓"
	}
	right := mutedStyle.Render(count+" · ") + barStyle.Render(m.sortKey()+" "+arrow)
	in := m.input
	in.Width = max(10, m.width-lipgloss.Width(right)-len(in.Prompt)-2)
	return spread(in.View(), right, m.width)
}

func (m model) footerView() string {
	if m.status != "" {
		return errorStyle.Render(m.status)
	}
	toggle := ""
	if m.catalog.HasSessions() {
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
	width := m.width - lipgloss.Width(bar)
	sortKey := m.sortKey()
	updated := recencyStyle(it.itemUpdated(), now).Bold(sortKey == "updated").Render(relativeTime(it.itemUpdated(), now))
	left, right := it.itemMeta(m.q, sortKey, now)
	if m.labelSources {
		left = append([]string{sourceStyles[it.itemSource()].Render(it.itemSource())}, left...)
	}
	lines := []string{
		bar + spread(m.q.highlight(it.itemTitle(), title), updated, width),
		bar + spread(strings.Join(left, "  "), strings.Join(right, mutedStyle.Render(" · ")), width),
	}
	if s := it.itemSnippet(); s != "" {
		lines = append(lines, bar+m.q.highlight(s, mutedStyle))
	}
	if selected {
		for i, l := range lines {
			lines[i] = fillRow(l, m.width)
		}
	}
	return append(lines, "")
}

func fillRow(line string, width int) string {
	line = ansi.Truncate(line, width, "…")
	line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
	// lipgloss ends every styled segment with a full reset, which would drop the row background after the first one
	parts := strings.Split(line, "\x1b[0m")
	for i, p := range parts {
		parts[i] = rowStyle.Render(p)
	}
	return strings.Join(parts, "")
}

func spread(left, right string, width int) string {
	rw := lipgloss.Width(right)
	left = ansi.Truncate(left, max(0, width-rw-1), "…")
	return left + strings.Repeat(" ", max(1, width-lipgloss.Width(left)-rw)) + right
}

func projectStyle(name string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(name))
	return lipgloss.NewStyle().Foreground(projectPalette[h.Sum32()%uint32(len(projectPalette))])
}

func recencyStyle(t, now time.Time) lipgloss.Style {
	switch d := now.Sub(t); {
	case d < time.Hour:
		return freshStyle
	case d < 24*time.Hour:
		return plainStyle
	}
	return mutedStyle
}

func metaStyle(emphasized bool) lipgloss.Style {
	if emphasized {
		return emphasisStyle
	}
	return mutedStyle
}

func (p Plan) itemKey() string        { return p.Name }
func (p Plan) itemSource() string     { return p.Source }
func (p Plan) itemTitle() string      { return p.Title }
func (p Plan) itemUpdated() time.Time { return p.Updated }
func (p Plan) itemSnippet() string    { return p.Snippet }

func (p Plan) itemMeta(q query, sortKey string, now time.Time) (left, right []string) {
	if p.Project != "" {
		left = append(left, q.highlight(p.Project, projectStyle(p.Project).Bold(sortKey == "project")))
	}
	left = append(left, q.highlight(p.Name, mutedStyle))
	right = []string{
		metaStyle(sortKey == "size").Render(formatSize(p.Size)),
		metaStyle(sortKey == "created").Render("created " + p.createdLabel(now)),
	}
	if p.Status != "" {
		right = append([]string{statusStyles[p.Status].Render(string(p.Status))}, right...)
	}
	return left, right
}

func (p Plan) createdLabel(now time.Time) string {
	if p.CreatedExact {
		return relativeTime(p.Created, now)
	}
	return "~" + relativeTime(p.Created, now)
}

func (s Session) itemKey() string        { return s.ID }
func (s Session) itemSource() string     { return s.Source }
func (s Session) itemTitle() string      { return s.Title }
func (s Session) itemUpdated() time.Time { return s.Updated }
func (s Session) itemSnippet() string    { return s.Snippet }

func (s Session) itemMeta(q query, sortKey string, now time.Time) (left, right []string) {
	if s.Project != "" {
		left = append(left, q.highlight(s.Project, projectStyle(s.Project).Bold(sortKey == "project")))
	}
	if s.Branch != "" {
		left = append(left, q.highlight(s.Branch, mutedStyle))
	}
	right = []string{
		metaStyle(sortKey == "messages").Render(messageCount(s.Messages)),
		metaStyle(sortKey == "created").Render("created " + relativeTime(s.Created, now)),
	}
	return left, right
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
