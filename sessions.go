package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const titleMaxLen = 80

var (
	errSessionNotFound = errors.New("session not found")
	wrapperRe          = regexp.MustCompile(`(?s)<(?:system-reminder|local-command-caveat|local-command-stdout|local-command-stderr)>.*?</(?:system-reminder|local-command-caveat|local-command-stdout|local-command-stderr)>`)
	commandTagRe       = regexp.MustCompile(`(?s)<command-(name|message|args)>(.*?)</command-(?:name|message|args)>`)
	safeShellRe        = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)
)

type turn struct {
	user bool
	at   time.Time
	text string
}

type Session struct {
	ID       string    `json:"id"`
	Source   string    `json:"source"`
	Title    string    `json:"title"`
	Project  string    `json:"project,omitempty"`
	Cwd      string    `json:"cwd,omitempty"`
	Branch   string    `json:"branch,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Messages int       `json:"messages"`
	Snippet  string    `json:"snippet,omitempty"`
	turns    []turn
	body     []byte
}

type sessionPlan struct {
	at   time.Time
	text string
}

type planStatus string

const (
	statusDraft    planStatus = "draft"
	statusPending  planStatus = "pending"
	statusRejected planStatus = "rejected"
	statusApproved planStatus = "approved"
	statusWorked   planStatus = "worked on"
)

type planReview struct {
	key     string
	status  planStatus
	at      time.Time
	toolID  string
	replied bool
}

type sessionBuilder struct {
	id                   string
	cwd, branch          string
	customTitle, aiTitle string
	firstPrompt          string
	created, updated     time.Time
	turns                []turn
	plans                []sessionPlan
	planFile             string
	reviews              []planReview
	children             []string
	body                 []byte
}

func (r planReview) awaitingWork() bool {
	return r.status == statusApproved || r.status == statusPending && r.replied
}

func (r *planReview) set(status planStatus, at time.Time) {
	r.status, r.at, r.toolID = status, at, ""
}

func (b *sessionBuilder) present(key, toolID string, at time.Time) {
	r := planReview{key: key, status: statusPending, at: at, toolID: toolID}
	for i := range b.reviews {
		if b.reviews[i].key == key {
			b.reviews[i] = r
			return
		}
	}
	b.reviews = append(b.reviews, r)
}

func (b *sessionBuilder) resolve(toolID string, approved bool, at time.Time) {
	status := statusRejected
	if approved {
		status = statusApproved
	}
	for i := range b.reviews {
		if r := &b.reviews[i]; r.status == statusPending && r.toolID == toolID {
			r.set(status, at)
		}
	}
}

func (b *sessionBuilder) settlePending(status planStatus, at time.Time) {
	for i := range b.reviews {
		if r := &b.reviews[i]; r.status == statusPending {
			r.set(status, at)
		}
	}
}

func (b *sessionBuilder) replyPending() {
	for i := range b.reviews {
		if r := &b.reviews[i]; r.status == statusPending {
			r.replied = true
		}
	}
}

func (b *sessionBuilder) work(at time.Time) {
	for i := range b.reviews {
		if r := &b.reviews[i]; r.awaitingWork() {
			r.set(statusWorked, at)
		}
	}
}

func (b *sessionBuilder) hasPending() bool {
	return slices.ContainsFunc(b.reviews, func(r planReview) bool { return r.status == statusPending })
}

func (b *sessionBuilder) awaitingWork() bool {
	return slices.ContainsFunc(b.reviews, planReview.awaitingWork)
}

func (b *sessionBuilder) awaitingResult(line []byte) bool {
	return slices.ContainsFunc(b.reviews, func(r planReview) bool {
		return r.status == statusPending && r.toolID != "" && bytes.Contains(line, []byte(r.toolID))
	})
}

func (b *sessionBuilder) review(key string) (planReview, bool) {
	i := slices.IndexFunc(b.reviews, func(r planReview) bool { return r.key == key })
	if i < 0 {
		return planReview{}, false
	}
	return b.reviews[i], true
}

func (b *sessionBuilder) add(rec *transcriptRecord) {
	switch rec.Type {
	case "custom-title":
		if t := strings.TrimSpace(rec.CustomTitle); t != "" {
			b.customTitle = t
		}
	case "ai-title":
		if t := strings.TrimSpace(rec.AITitle); t != "" {
			b.aiTitle = t
		}
	case "user", "assistant":
		if rec.IsSidechain || rec.IsMeta || rec.IsCompactSummary {
			return
		}
		text := recordText(rec)
		if text == "" {
			return
		}
		if b.cwd == "" {
			b.cwd = rec.Cwd
		}
		if rec.GitBranch != "" {
			b.branch = rec.GitBranch
		}
		b.addTurn(rec.Type == "user", rec.Timestamp, text)
	}
}

func (b *sessionBuilder) addTurn(user bool, at time.Time, text string) {
	if text == "" {
		return
	}
	if !at.IsZero() {
		if b.created.IsZero() || at.Before(b.created) {
			b.created = at
		}
		if at.After(b.updated) {
			b.updated = at
		}
	}
	if user && b.firstPrompt == "" {
		b.firstPrompt = text
	}
	if len(b.body) > 0 {
		b.body = append(b.body, "\n\n"...)
	}
	b.body = append(b.body, text...)
	if n := len(b.turns); n > 0 && !user && !b.turns[n-1].user {
		b.turns[n-1].text += "\n\n" + text
		return
	}
	b.turns = append(b.turns, turn{user: user, at: at, text: text})
}

func (b *sessionBuilder) title() string {
	switch {
	case b.customTitle != "":
		return b.customTitle
	case b.aiTitle != "":
		return b.aiTitle
	}
	return shortTitle(b.firstPrompt)
}

func shortTitle(s string) string {
	t := strings.TrimSpace(whitespaceRe.ReplaceAllString(s, " "))
	if utf8.RuneCountInString(t) > titleMaxLen {
		t = string([]rune(t)[:titleMaxLen]) + "…"
	}
	return t
}

func (b *sessionBuilder) snapshot(withTurns bool) Session {
	s := Session{
		ID:       b.id,
		Title:    b.title(),
		Project:  projectName(b.cwd),
		Cwd:      b.cwd,
		Branch:   b.branch,
		Created:  b.created,
		Updated:  b.updated,
		Messages: len(b.turns),
		body:     b.body[:len(b.body):len(b.body)],
	}
	if withTurns {
		s.turns = slices.Clone(b.turns)
	}
	return s
}

func recordText(rec *transcriptRecord) string {
	var parts []string
	var s string
	if json.Unmarshal(rec.Message.Content, &s) == nil {
		parts = append(parts, s)
	} else {
		var blocks []contentBlock
		if json.Unmarshal(rec.Message.Content, &blocks) != nil {
			return ""
		}
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
	}
	text := strings.Join(parts, "\n\n")
	if rec.Type == "user" {
		return promptText(text)
	}
	return strings.TrimSpace(text)
}

func promptText(s string) string {
	s = wrapperRe.ReplaceAllString(s, "")
	var name, args string
	for _, m := range commandTagRe.FindAllStringSubmatch(s, -1) {
		switch m[1] {
		case "name":
			name = strings.TrimSpace(m[2])
		case "args":
			args = strings.TrimSpace(m[2])
		}
	}
	s = strings.TrimSpace(commandTagRe.ReplaceAllString(s, ""))
	if args != "" {
		s = strings.TrimSpace(name + " " + args + "\n\n" + s)
	}
	if strings.HasPrefix(s, "[Request interrupted by user") {
		return ""
	}
	return s
}

func (s Session) markdown() []byte {
	var b bytes.Buffer
	for i, t := range s.turns {
		if i > 0 {
			b.WriteString("\n")
		}
		who := s.agentName()
		if t.user {
			who = "You"
		}
		fmt.Fprintf(&b, "## %s · %s\n\n%s\n", who, t.at.Local().Format("Jan 2, 2006 15:04"), t.text)
	}
	return b.Bytes()
}

func (s Session) agentName() string {
	switch s.Source {
	case sourceCodex:
		return "Codex"
	case sourceAgy:
		return "Antigravity"
	}
	return "Claude"
}

func (s Session) resumeCommand(home string) string {
	var cmd string
	switch s.Source {
	case sourceCodex:
		cmd = "codex resume " + shellQuote(s.ID)
	case sourceAgy:
		cmd = "agy --conversation=" + shellQuote(s.ID)
	default:
		cmd = "claude --resume " + shellQuote(s.ID)
	}
	if s.Cwd == "" {
		return cmd
	}
	return "cd " + shellPath(s.Cwd, home) + " && " + cmd
}

func shellPath(p, home string) string {
	home = strings.TrimSuffix(home, "/")
	if home == "" {
		return shellQuote(p)
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+"/"); ok {
		return "~/" + shellQuote(rest)
	}
	return shellQuote(p)
}

func shellQuote(s string) string {
	if safeShellRe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
