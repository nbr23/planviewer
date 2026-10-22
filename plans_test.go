package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"a.md", "# Persian rugs\n\nbody", "Persian rugs"},
		{"a.md", "\n\n  ## Plan: MVP  \nmore", "Plan: MVP"},
		{"a.md", "#NoSpace", "NoSpace"},
		{"a.md", "Plain first line\n# Heading", "Plain first line"},
		{"a.md", "\r\n# Windows\r\n", "Windows"},
		{"fallback-name.md", "####   \nreal", "fallback-name"},
		{"empty.md", "", "empty"},
	}
	for _, tt := range tests {
		if got := extractTitle(tt.name, []byte(tt.body)); got != tt.want {
			t.Errorf("extractTitle(%q, %q) = %q, want %q", tt.name, tt.body, got, tt.want)
		}
	}
}

func TestValidName(t *testing.T) {
	valid := []string{"cosmic-honking-neumann.md", "a.md", "v1.2_notes.md"}
	invalid := []string{"", ".md", ".hidden.md", "../etc/passwd", "../x.md", "a/b.md", "x.txt", "x.md/", "x md.md"}
	for _, n := range valid {
		if !validName(n) {
			t.Errorf("validName(%q) = false, want true", n)
		}
	}
	for _, n := range invalid {
		if validName(n) {
			t.Errorf("validName(%q) = true, want false", n)
		}
	}
}

func TestStoreListAndGet(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("one.md", "# One\n")
	write("notes.txt", "ignored")
	if err := os.Symlink(filepath.Join(dir, "one.md"), filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir, nil)
	plans, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Name != "one.md" || plans[0].Title != "One" || plans[0].CreatedExact {
		t.Fatalf("List() = %+v", plans)
	}

	write("one.md", "# One, revised\n")
	if p, err := s.Get("one.md"); err != nil || p.Title != "One, revised" {
		t.Fatalf("Get after rewrite = %+v, %v", p, err)
	}
	for _, name := range []string{"link.md", "missing.md", "notes.txt", "../one.md"} {
		if _, err := s.Get(name); err != errNotFound {
			t.Errorf("Get(%q) err = %v, want errNotFound", name, err)
		}
	}

	os.Remove(filepath.Join(dir, "one.md"))
	if plans, _ := s.List(); len(plans) != 0 || len(s.cache) != 0 {
		t.Fatalf("after delete: plans=%d cache=%d", len(plans), len(s.cache))
	}
}

func TestFilterPlans(t *testing.T) {
	plans := func() []Plan {
		return []Plan{
			{Name: "a.md", Source: sourceClaude, Title: "Goodreads live feed", Status: statusWorked, body: []byte("# Goodreads live feed\nnothing")},
			{Name: "b.md", Source: sourceCodex, Title: "Persian rugs", Project: "vacuum", Status: statusPending, body: []byte("# Persian rugs\nThe fringe catches on the BRUSH roll.")},
		}
	}
	tests := []struct {
		q       string
		want    []string
		snippet bool
	}{
		{"", []string{"a.md", "b.md"}, false},
		{"goodreads", []string{"a.md"}, false},
		{"brush", []string{"b.md"}, true},
		{"rugs brush", []string{"b.md"}, true},
		{"vacuum", []string{"b.md"}, false},
		{"rugs goodreads", nil, false},
		{"a+b(", nil, false},
		{"src:codex", []string{"b.md"}, false},
		{"src:cl", []string{"a.md"}, false},
		{"src:claude brush", nil, false},
		{"src: goodreads", []string{"a.md"}, false},
		{"status:worked", []string{"a.md"}, false},
		{"status:PEND brush", []string{"b.md"}, true},
		{"status:draft", nil, false},
		{"status: rugs", []string{"b.md"}, false},
	}
	for _, tt := range tests {
		got := filterPlans(plans(), parseQuery(tt.q))
		var names []string
		for _, p := range got {
			names = append(names, p.Name)
			if (p.Snippet != "") != tt.snippet {
				t.Errorf("q=%q %s snippet=%q, want snippet=%v", tt.q, p.Name, p.Snippet, tt.snippet)
			}
		}
		if len(names) != len(tt.want) || (len(names) > 0 && names[0] != tt.want[0]) {
			t.Errorf("q=%q got %v, want %v", tt.q, names, tt.want)
		}
	}
}

func TestSnippetRuneBoundaries(t *testing.T) {
	body := []byte("ééééééééééééééééééééééééééééééééééééééééééééééééééééééééééééé target")
	at := len(body) - len("target")
	s := snippet(body, at)
	if s[:len("…")] != "…" || s[len(s)-len("target"):] != "target" {
		t.Fatalf("snippet = %q", s)
	}
}
