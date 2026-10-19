package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestServerAgyRoutes(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	idx := NewAgyIndex(agyFixture(t), 0)
	srv, err := NewServer(NewCatalog(nil, newArtifactStore(idx), idx))
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	get := func(path string) (int, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code, rec.Body.String()
	}

	name := agyMain + "/implementation_plan.md"
	if code, body := get("/p/" + url.PathEscape(name)); code != http.StatusOK || !strings.Contains(body, "Vacuum first.") || !strings.Contains(body, `href="/raw/`+name+`"`) {
		t.Fatalf("plan page: %d\n%s", code, body)
	}
	if code, body := get("/raw/" + name); code != http.StatusOK || body != "# Rug plan\n\nVacuum first.\n" {
		t.Fatalf("raw: %d %q", code, body)
	}
	if code, body := get("/s/" + agyMain); code != http.StatusOK || !strings.Contains(body, "agy --conversation="+agyMain) || !strings.Contains(body, "<h2>Antigravity") {
		t.Fatalf("session page: %d\n%s", code, body)
	}
	for _, path := range []string{"/raw/" + agyMain + "/notes.md", "/raw/" + agyMain + "/../../cache/conversation_metadata.json", "/s/" + agySub} {
		if code, _ := get(path); code == http.StatusOK {
			t.Errorf("GET %s = 200", path)
		}
	}
}
