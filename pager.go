package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

const (
	maxWrap          = 120
	pagerHeaderLines = 3
	pagerFooterLines = 1
	matchContext     = 2
)

type renderKey struct {
	name    string
	width   int
	updated time.Time
}

type pager struct {
	style    string
	vp       viewport.Model
	plan     Plan
	q        query
	width    int
	rendered renderKey
	err      error
	matches  []int
	match    int
}

func newPager(style string) pager {
	return pager{style: style, vp: viewport.New(0, 0)}
}

func (p *pager) setSize(w, h int) {
	p.width = w
	p.vp.Width = w
	p.vp.Height = max(1, h-pagerHeaderLines-pagerFooterLines)
	if p.plan.Name != "" {
		p.render()
	}
}

func (p *pager) open(plan Plan, q query) {
	p.plan, p.q = plan, q
	p.render()
	p.vp.GotoTop()
	p.match = 0
	if len(p.matches) > 0 {
		p.vp.SetYOffset(p.matches[0] - matchContext)
	}
}

func (p *pager) render() {
	wrap := min(p.width, maxWrap)
	key := renderKey{p.plan.Name, wrap, p.plan.Updated}
	if key == p.rendered {
		return
	}
	p.rendered = key
	out, err := p.renderMarkdown(wrap)
	p.err = err
	if err != nil {
		out = string(p.plan.body)
	}
	p.vp.SetContent(out)
	p.matches = p.findMatches(out)
	p.match = min(p.match, max(len(p.matches)-1, 0))
}

func (p *pager) renderMarkdown(wrap int) (string, error) {
	r, err := glamour.NewTermRenderer(glamour.WithStylePath(p.style), glamour.WithWordWrap(max(20, wrap-4)))
	if err != nil {
		return "", err
	}
	return r.Render(string(p.plan.body))
}

func (p *pager) findMatches(content string) []int {
	if p.q.empty() {
		return nil
	}
	var lines []int
	for i, line := range strings.Split(content, "\n") {
		plain := ansi.Strip(line)
		for _, re := range p.q.terms {
			if re.MatchString(plain) {
				lines = append(lines, i)
				break
			}
		}
	}
	return lines
}

func (p *pager) jump(delta int) {
	if len(p.matches) == 0 {
		return
	}
	p.match = (p.match + delta + len(p.matches)) % len(p.matches)
	p.vp.SetYOffset(p.matches[p.match] - matchContext)
}

func (p pager) Update(msg tea.Msg) (pager, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "g", "home":
			p.vp.GotoTop()
			return p, nil
		case "G", "end":
			p.vp.GotoBottom()
			return p, nil
		case "n":
			p.jump(1)
			return p, nil
		case "N":
			p.jump(-1)
			return p, nil
		}
	}
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return p, cmd
}

func (p pager) View() string {
	return strings.Join([]string{p.headerView(), p.vp.View(), p.footerView()}, "\n")
}

func (p pager) headerView() string {
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
	lines := []string{
		selectedStyle.Render(p.plan.Title),
		mutedStyle.Render(strings.Join(meta, " · ")),
		mutedStyle.Render(strings.Repeat("─", p.width)),
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, p.width, "…")
	}
	return strings.Join(lines, "\n")
}

func (p pager) footerView() string {
	parts := []string{fmt.Sprintf("%3.f%%", p.vp.ScrollPercent()*100)}
	if p.err != nil {
		parts = append(parts, errorStyle.Render("render failed, showing raw: "+p.err.Error()))
	}
	if len(p.matches) > 0 {
		parts = append(parts, fmt.Sprintf("match %d/%d · n/N next/prev", p.match+1, len(p.matches)))
	}
	parts = append(parts, "j/k scroll · space/b page · g/G top/bottom · q back")
	return ansi.Truncate(mutedStyle.Render(strings.Join(parts, " · ")), p.width, "…")
}
