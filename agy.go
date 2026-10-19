package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	agyIDRe           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	agyChildRe        = regexp.MustCompile(`"conversationId":\s*"([0-9a-f-]{36})"`)
	agyTitleRe        = regexp.MustCompile(`title:\s*("(?:[^"\\]|\\.)*")`)
	agyFullHead       = []byte(`{"step_index":0,`)
	agyTurnMarkers    = [][]byte{[]byte(`"type":"USER_INPUT"`), []byte(`"type":"PLANNER_RESPONSE"`)}
	agyContentMarker  = []byte(`"content":"`)
	agySpawnMarker    = []byte("Created the following subagents")
	agyArtifactMarker = []byte(`"ArtifactMetadata"`)
)

type agyFormat struct{}

type agyStep struct {
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Content   string    `json:"content"`
	ToolCalls []struct {
		Args struct {
			TargetFile       string          `json:"TargetFile"`
			ArtifactMetadata json.RawMessage `json:"ArtifactMetadata"`
		} `json:"args"`
	} `json:"tool_calls"`
}

type fileStamp struct {
	mtime time.Time
	size  int64
}

type agyConversation struct {
	title, preview, cwd string
	internal            bool
}

type agyMeta struct {
	dir         string
	stamps      map[string]fileStamp
	convs       map[string]agyConversation
	history     map[string]string
	annotations map[string]string
}

type artifactFile struct {
	stamp, metaStamp fileStamp
	title            string
	updated          time.Time
	body             []byte
}

type artifactStore struct {
	root  string
	index *TranscriptIndex
	mu    sync.Mutex
	cache map[string]*artifactFile
}

func NewAgyIndex(dir string, interval time.Duration) *TranscriptIndex {
	t := newTranscriptIndex(filepath.Join(dir, "brain"), agyFormat{}, interval)
	t.meta = &agyMeta{
		dir:         dir,
		stamps:      map[string]fileStamp{},
		annotations: map[string]string{},
	}
	return t
}

func (agyFormat) source() string { return sourceAgy }

func (agyFormat) files(root string) []string {
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range dirs {
		if !d.IsDir() || !agyIDRe.MatchString(d.Name()) {
			continue
		}
		logs := filepath.Join(root, d.Name(), ".system_generated", "logs")
		// transcript.jsonl cuts long content to ~4KB; transcript_full.jsonl keeps it but can lose its oldest steps.
		if full := filepath.Join(logs, "transcript_full.jsonl"); hasPrefix(full, agyFullHead) {
			out = append(out, full)
		} else {
			out = append(out, filepath.Join(logs, "transcript.jsonl"))
		}
	}
	return out
}

func hasPrefix(p string, prefix []byte) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(prefix))
	_, err = io.ReadFull(f, buf)
	return err == nil && bytes.Equal(buf, prefix)
}

func (agyFormat) open(t *TranscriptIndex, p string) *sessionBuilder {
	return t.addSession(filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(p)))))
}

func (agyFormat) indexLine(t *TranscriptIndex, line []byte, tf *transcriptFile) {
	turn := containsAny(line, agyTurnMarkers) && bytes.Contains(line, agyContentMarker)
	spawn := bytes.Contains(line, agySpawnMarker)
	artifact := bytes.Contains(line, agyArtifactMarker)
	if !turn && !spawn && !artifact {
		return
	}
	var st agyStep
	if json.Unmarshal(line, &st) != nil {
		return
	}
	b := tf.session
	switch st.Type {
	case "USER_INPUT":
		b.addTurn(true, st.CreatedAt, agyPrompt(st.Content))
	case "PLANNER_RESPONSE":
		b.addTurn(false, st.CreatedAt, strings.TrimSpace(st.Content))
	default:
		if spawn {
			for _, m := range agyChildRe.FindAllStringSubmatch(st.Content, -1) {
				b.children = append(b.children, m[1])
			}
		}
	}
	if artifact {
		indexArtifacts(t, &st)
	}
}

func agyPrompt(s string) string {
	if _, rest, ok := strings.Cut(s, "<USER_REQUEST>"); ok {
		s, _, _ = strings.Cut(rest, "</USER_REQUEST>")
	}
	return strings.TrimSpace(s)
}

func indexArtifacts(t *TranscriptIndex, st *agyStep) {
	if st.CreatedAt.IsZero() {
		return
	}
	for _, tc := range st.ToolCalls {
		target, meta := tc.Args.TargetFile, string(tc.Args.ArtifactMetadata)
		if target == "" || meta == "" || meta == "null" {
			continue
		}
		if strings.HasPrefix(target, `"`) && json.Unmarshal([]byte(target), &target) != nil {
			continue
		}
		dir := path.Dir(target)
		if path.Base(path.Dir(dir)) != "brain" {
			continue
		}
		key := path.Base(dir) + "/" + path.Base(target)
		if cur, ok := t.origins[key]; ok && !st.CreatedAt.Before(cur.Created) {
			continue
		}
		t.origins[key] = Origin{Created: st.CreatedAt}
	}
}

func stampOf(info fs.FileInfo) fileStamp {
	return fileStamp{mtime: info.ModTime(), size: info.Size()}
}

func (s fileStamp) same(o fileStamp) bool {
	return s.mtime.Equal(o.mtime) && s.size == o.size
}

func (m *agyMeta) refresh() {
	if p := filepath.Join(m.dir, "cache", "conversation_metadata.json"); m.changed(p) {
		m.convs = readAgyConversations(p)
	}
	if p := filepath.Join(m.dir, "history.jsonl"); m.changed(p) {
		m.history = readAgyHistory(p)
	}
	m.refreshAnnotations()
}

func (m *agyMeta) changed(p string) bool {
	var st fileStamp
	if info, err := os.Stat(p); err == nil {
		st = stampOf(info)
	}
	if cur, ok := m.stamps[p]; ok && cur.same(st) {
		return false
	}
	m.stamps[p] = st
	return true
}

func (m *agyMeta) refreshAnnotations() {
	dir := filepath.Join(m.dir, "annotations")
	entries, _ := os.ReadDir(dir)
	seen := map[string]bool{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".pbtxt")
		if !ok || !e.Type().IsRegular() {
			continue
		}
		seen[id] = true
		if p := filepath.Join(dir, e.Name()); m.changed(p) {
			m.annotations[id] = readAgyAnnotation(p)
		}
	}
	for id := range m.annotations {
		if !seen[id] {
			delete(m.annotations, id)
			delete(m.stamps, filepath.Join(dir, id+".pbtxt"))
		}
	}
}

func (m *agyMeta) annotate(s *Session) bool {
	c := m.convs[s.ID]
	for _, title := range []string{m.annotations[s.ID], c.title, shortTitle(c.preview)} {
		if title != "" {
			s.Title = title
			break
		}
	}
	if s.Cwd == "" {
		s.Cwd = cmp.Or(c.cwd, m.history[s.ID])
		s.Project = projectName(s.Cwd)
	}
	return !c.internal
}

func readAgyConversations(p string) map[string]agyConversation {
	var doc struct {
		Conversations map[string]struct {
			IsInternal bool `json:"is_internal"`
			Summary    struct {
				Title         string   `json:"Title"`
				Preview       string   `json:"Preview"`
				WorkspaceURIs []string `json:"WorkspaceURIs"`
			} `json:"summary"`
		} `json:"conversations"`
	}
	raw, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	out := make(map[string]agyConversation, len(doc.Conversations))
	for id, c := range doc.Conversations {
		conv := agyConversation{
			title:    strings.TrimSpace(c.Summary.Title),
			preview:  c.Summary.Preview,
			internal: c.IsInternal,
		}
		if len(c.Summary.WorkspaceURIs) > 0 {
			conv.cwd = fileURIPath(c.Summary.WorkspaceURIs[0])
		}
		out[id] = conv
	}
	return out
}

func fileURIPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}

func readAgyHistory(p string) map[string]string {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		var entry struct {
			Workspace      string `json:"workspace"`
			ConversationID string `json:"conversationId"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.ConversationID != "" && entry.Workspace != "" {
			out[entry.ConversationID] = entry.Workspace
		}
		if err != nil {
			break
		}
	}
	return out
}

func readAgyAnnotation(p string) string {
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	m := agyTitleRe.FindSubmatch(raw)
	if m == nil {
		return ""
	}
	title, err := strconv.Unquote(strings.ReplaceAll(string(m[1]), `\'`, `'`))
	if err != nil {
		title = string(m[1][1 : len(m[1])-1])
	}
	return strings.TrimSpace(title)
}

func newArtifactStore(index *TranscriptIndex) *artifactStore {
	return &artifactStore{root: index.root, index: index, cache: map[string]*artifactFile{}}
}

func (a *artifactStore) List() ([]Plan, error) {
	a.index.Refresh()
	dirs, err := os.ReadDir(a.root)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := map[string]bool{}
	var plans []Plan
	for _, d := range dirs {
		if !d.IsDir() || !agyIDRe.MatchString(d.Name()) {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(a.root, d.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !validName(e.Name()) {
				continue
			}
			name := d.Name() + "/" + e.Name()
			af, err := a.load(name)
			if err != nil {
				continue
			}
			seen[name] = true
			plans = append(plans, a.plan(name, af))
		}
	}
	for name := range a.cache {
		if !seen[name] {
			delete(a.cache, name)
		}
	}
	return plans, nil
}

func (a *artifactStore) Get(name string) (Plan, error) {
	id, file, ok := strings.Cut(name, "/")
	if !ok || !agyIDRe.MatchString(id) || !validName(file) {
		return Plan{}, errNotFound
	}
	a.index.Refresh()
	a.mu.Lock()
	defer a.mu.Unlock()
	af, err := a.load(name)
	if err != nil {
		return Plan{}, errNotFound
	}
	return a.plan(name, af), nil
}

func (a *artifactStore) load(name string) (*artifactFile, error) {
	p := filepath.Join(a.root, filepath.FromSlash(name))
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errNotFound
	}
	metaPath := p + ".metadata.json"
	metaInfo, err := os.Stat(metaPath)
	if err != nil {
		return nil, errNotFound
	}
	st, mst := stampOf(info), stampOf(metaInfo)
	if af, ok := a.cache[name]; ok && af.stamp.same(st) && af.metaStamp.same(mst) {
		return af, nil
	}
	body, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	af := &artifactFile{
		stamp:     st,
		metaStamp: mst,
		title:     extractTitle(path.Base(name), body),
		updated:   info.ModTime().UTC(),
		body:      body,
	}
	var meta struct {
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if raw, err := os.ReadFile(metaPath); err == nil && json.Unmarshal(raw, &meta) == nil && !meta.UpdatedAt.IsZero() {
		af.updated = meta.UpdatedAt
	}
	a.cache[name] = af
	return af, nil
}

func (a *artifactStore) plan(name string, af *artifactFile) Plan {
	id, _, _ := strings.Cut(name, "/")
	p := Plan{
		Name:    name,
		Source:  sourceAgy,
		Title:   af.title,
		Created: af.updated,
		Updated: af.updated,
		Project: projectName(a.index.cwd(id)),
		Size:    int64(len(af.body)),
		body:    af.body,
	}
	if o, ok := a.index.Lookup(name); ok {
		p.Created, p.CreatedExact = o.Created, true
	}
	return p
}
