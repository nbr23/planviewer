package main

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	snippetBefore = 60
	snippetAfter  = 120
)

var whitespaceRe = regexp.MustCompile(`\s+`)

type query struct {
	terms []*regexp.Regexp
}

func parseQuery(q string) query {
	var qu query
	for _, f := range strings.Fields(q) {
		qu.terms = append(qu.terms, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(f)))
	}
	return qu
}

func (q query) empty() bool {
	return len(q.terms) == 0
}

func (q query) match(p *Plan) bool {
	snippetAt := -1
	for _, re := range q.terms {
		if re.MatchString(p.Title) || re.MatchString(p.Name) || re.MatchString(p.Project) {
			continue
		}
		loc := re.FindIndex(p.body)
		if loc == nil {
			return false
		}
		if snippetAt < 0 {
			snippetAt = loc[0]
		}
	}
	if snippetAt >= 0 {
		p.Snippet = snippet(p.body, snippetAt)
	}
	return true
}

func filterPlans(plans []Plan, q query) []Plan {
	if q.empty() {
		return plans
	}
	out := plans[:0]
	for i := range plans {
		if q.match(&plans[i]) {
			out = append(out, plans[i])
		}
	}
	return out
}

func snippet(body []byte, at int) string {
	start := max(0, at-snippetBefore)
	end := min(len(body), at+snippetAfter)
	for start > 0 && !utf8.RuneStart(body[start]) {
		start--
	}
	for end < len(body) && !utf8.RuneStart(body[end]) {
		end++
	}
	s := strings.TrimSpace(whitespaceRe.ReplaceAllString(string(body[start:end]), " "))
	if start > 0 {
		s = "…" + s
	}
	if end < len(body) {
		s += "…"
	}
	return s
}
