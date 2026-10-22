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
	terms  []*regexp.Regexp
	source string
	status string
}

func parseQuery(q string) query {
	var qu query
	for _, f := range strings.Fields(q) {
		if src, ok := strings.CutPrefix(f, "src:"); ok {
			qu.source = strings.ToLower(src)
			continue
		}
		if st, ok := strings.CutPrefix(f, "status:"); ok {
			qu.status = strings.ToLower(st)
			continue
		}
		qu.terms = append(qu.terms, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(f)))
	}
	return qu
}

func (q query) empty() bool {
	return len(q.terms) == 0 && q.source == "" && q.status == ""
}

func (q query) allows(source string) bool {
	return strings.HasPrefix(source, q.source)
}

func (q query) match(fields []string, body []byte) (string, bool) {
	snippetAt := -1
	for _, re := range q.terms {
		if matchAny(re, fields) {
			continue
		}
		loc := re.FindIndex(body)
		if loc == nil {
			return "", false
		}
		if snippetAt < 0 {
			snippetAt = loc[0]
		}
	}
	if snippetAt < 0 {
		return "", true
	}
	return snippet(body, snippetAt), true
}

func matchAny(re *regexp.Regexp, fields []string) bool {
	for _, f := range fields {
		if re.MatchString(f) {
			return true
		}
	}
	return false
}

func filterPlans(plans []Plan, q query) []Plan {
	if q.empty() {
		return plans
	}
	out := plans[:0]
	for _, p := range plans {
		if !q.allows(p.Source) || !strings.HasPrefix(string(p.Status), q.status) {
			continue
		}
		if s, ok := q.match([]string{p.Title, p.Name, p.Project}, p.body); ok {
			p.Snippet = s
			out = append(out, p)
		}
	}
	return out
}

func filterSessions(sessions []Session, q query) []Session {
	if q.empty() {
		return sessions
	}
	out := sessions[:0]
	for _, s := range sessions {
		if !q.allows(s.Source) {
			continue
		}
		if snip, ok := q.match([]string{s.Title, s.Project, s.Branch, s.ID}, s.body); ok {
			s.Snippet = snip
			out = append(out, s)
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
