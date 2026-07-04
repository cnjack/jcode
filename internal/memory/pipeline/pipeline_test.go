package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory"
	"github.com/cnjack/jcode/internal/session"
)

func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// writeSession writes a leader session file + index entry.
func writeSession(t *testing.T, home, project, uuid string, endTime string, entries []session.Entry) string {
	t.Helper()
	dir := filepath.Join(home, ".jcode", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		data, _ := json.Marshal(e)
		b.Write(data)
		b.WriteString("\n")
	}
	file := filepath.Join(dir, uuid+".json")
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// index
	idxPath := filepath.Join(dir, "session.json")
	idx := map[string]map[string][]session.SessionMeta{"sessions": {}}
	if data, err := os.ReadFile(idxPath); err == nil {
		_ = json.Unmarshal(data, &idx)
	}
	if idx["sessions"] == nil {
		idx["sessions"] = map[string][]session.SessionMeta{}
	}
	idx["sessions"][project] = append(idx["sessions"][project], session.SessionMeta{
		UUID: uuid, Project: project,
		StartTime: time.Now().Add(-time.Hour).Format(time.RFC3339),
		EndTime:   endTime, TerminalStatus: "success",
	})
	data, _ := json.MarshalIndent(idx, "", " ")
	if err := os.WriteFile(idxPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func chatEntries(userMsg string) []session.Entry {
	return []session.Entry{
		{Type: session.EntrySessionStart, Timestamp: "2026-07-04T10:00:00Z", Project: "/p"},
		{Type: session.EntryUser, Content: userMsg},
		{Type: session.EntryToolCall, Name: "write", Args: `{"file_path":"a.txt"}`},
		{Type: session.EntryToolResult, Name: "write", Output: "ok"},
		{Type: session.EntryAssistant, Content: "done, saved."},
	}
}

// stubModel returns a fixed response.
type stubModel struct {
	resp string
	err  error
	n    int
}

func (s *stubModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	s.n++
	if s.err != nil {
		return nil, s.err
	}
	return &schema.Message{Role: schema.Assistant, Content: s.resp}, nil
}

func TestBuildTranscript(t *testing.T) {
	home := setHome(t)
	file := writeSession(t, home, "/p", "u-1", time.Now().Format(time.RFC3339),
		append(chatEntries("please remember we use make test-fast, api_key = topsecret99"),
			session.Entry{Type: session.EntrySystemPrompt, Content: "SYSTEM SHOULD NOT APPEAR"},
			session.Entry{Type: session.EntryCompact, Summary: "earlier work summary"},
		))
	text, entries, err := buildTranscript(file, 100000)
	if err != nil {
		t.Fatal(err)
	}
	if entries < 5 {
		t.Fatalf("entries=%d", entries)
	}
	if strings.Contains(text, "SYSTEM SHOULD NOT APPEAR") {
		t.Error("system prompt leaked into transcript")
	}
	if strings.Contains(text, "topsecret99") {
		t.Error("transcript not redacted")
	}
	if !strings.Contains(text, "make test-fast") || !strings.Contains(text, "earlier work summary") {
		t.Errorf("transcript missing content:\n%s", text)
	}
	// tail-keeping truncation
	text2, _, _ := buildTranscript(file, 80)
	if len(text2) > 200 || !strings.Contains(text2, "truncated") {
		t.Errorf("truncation failed: %q", text2)
	}
}

func TestBuildTranscriptTooThin(t *testing.T) {
	home := setHome(t)
	file := writeSession(t, home, "/p", "u-thin", time.Now().Format(time.RFC3339),
		[]session.Entry{{Type: session.EntryAssistant, Content: "hello"}})
	text, _, err := buildTranscript(file, 100000)
	if err != nil || text != "" {
		t.Fatalf("thin session should be no-op, got %q err=%v", text, err)
	}
}

func TestSelectSessions(t *testing.T) {
	home := setHome(t)
	proj := "/proj/x"
	writeSession(t, home, proj, "ended-1", time.Now().Format(time.RFC3339), chatEntries("hi"))
	writeSession(t, home, proj, "running-1", "", chatEntries("hi"))

	st := &memory.State{Extracted: map[string]*memory.ExtractRecord{}}
	log := func(string, ...any) {}

	got := selectSessions(proj, st, 30, false, log)
	if len(got) != 1 || got[0].meta.UUID != "ended-1" {
		t.Fatalf("want only ended-1, got %+v", got)
	}
	// include-recent picks up the running one too
	got = selectSessions(proj, st, 30, true, log)
	if len(got) != 2 {
		t.Fatalf("include-recent should see 2, got %d", len(got))
	}
	// already extracted (newer than file) → skipped
	st.Extracted["ended-1"] = &memory.ExtractRecord{At: time.Now().Add(time.Hour).Format(time.RFC3339)}
	got = selectSessions(proj, st, 30, false, log)
	if len(got) != 0 {
		t.Fatalf("extracted session should be skipped, got %+v", got)
	}
}

func TestParseExtractJSON(t *testing.T) {
	res, err := parseExtractJSON("```json\n{\"summary\":\"s\",\"slug\":\"a-b\",\"memory\":\"- m\"}\n```")
	if err != nil || res.Slug != "a-b" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := parseExtractJSON("no json here"); err == nil {
		t.Error("expected error for non-JSON")
	}
}

func TestExtractWithStub(t *testing.T) {
	meta := session.SessionMeta{UUID: "u", StartTime: "2026-07-04T10:00:00Z", Project: "/p"}
	stub := &stubModel{resp: `{"summary":"did things","slug":"did-things","memory":"- user prefers tabs"}`}
	res, err := extract(context.Background(), stub, meta, "USER: hello")
	if err != nil || res.Memory == "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// hard failure surfaces
	bad := &stubModel{err: fmt.Errorf("boom")}
	if _, err := extract(context.Background(), bad, meta, "x"); err == nil {
		t.Error("expected model error")
	}
}

func TestPhase2NoDiffFastPath(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not installed")
	}
	setHome(t)
	proj := "/proj/noop"
	cfg := &config.Config{}
	// no sessions, empty scope → phase2 should take the no-op fast path
	if err := runPhase2(context.Background(), cfg, proj, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	st := memory.LoadState(memory.ProjectRoot(proj))
	if st.LastConsolidation == nil || !st.LastConsolidation.NoopFastPath {
		t.Fatalf("expected noop fast path, got %+v", st.LastConsolidation)
	}
	// state.json contains the assertable marker
	data, _ := os.ReadFile(filepath.Join(memory.ProjectRoot(proj), memory.StateFile))
	if !strings.Contains(string(data), "noop_fast_path") {
		t.Error("state.json missing noop_fast_path marker")
	}
}

func TestExpireAndRank(t *testing.T) {
	setHome(t)
	proj := "/proj/rank"
	scope := memory.ProjectRoot(proj)
	if err := memory.EnsureScope(scope); err != nil {
		t.Fatal(err)
	}
	mk := func(name string) string {
		rel := filepath.Join(memory.SummariesDir, name)
		if err := os.WriteFile(filepath.Join(scope, rel), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return rel
	}
	old := time.Now().AddDate(0, 0, -60).Format(time.RFC3339)
	fresh := time.Now().Format(time.RFC3339)
	usedRel := mk("used.md")
	_ = memory.UpdateState(scope, func(st *memory.State) error {
		st.Extracted = map[string]*memory.ExtractRecord{
			// "expired": extracted 60d ago, never read → usage falls back to At → expired
			"expired": {At: old, SummaryFile: mk("expired.md")},
			// "used": extracted 60d ago BUT read recently → usage bridge keeps it alive
			"used":  {At: old, SummaryFile: usedRel},
			"fresh": {At: fresh, SummaryFile: mk("fresh.md")},
		}
		// The usage signal lives in st.Files (written by RecordUsage), NOT on
		// ExtractRecord — this is exactly the bridge the fix introduced.
		st.Files[usedRel] = &memory.FileUsage{UsageCount: 5, LastUsage: fresh}
		return nil
	})
	st := memory.LoadState(scope)
	expireAndRank(scope, st, &config.Config{}, func(string, ...any) {})

	if _, err := os.Stat(filepath.Join(scope, memory.SummariesDir, "expired.md")); !os.IsNotExist(err) {
		t.Error("expired summary not removed")
	}
	for _, keep := range []string{"used.md", "fresh.md"} {
		if _, err := os.Stat(filepath.Join(scope, memory.SummariesDir, keep)); err != nil {
			t.Errorf("%s should survive (usage bridge should keep 'used' alive despite old At): %v", keep, err)
		}
	}
	st = memory.LoadState(scope)
	if _, ok := st.Extracted["expired"]; ok {
		t.Error("expired record not dropped from state")
	}
}

func TestBudgetGateSkipsPhase1(t *testing.T) {
	setHome(t)
	proj := "/proj/budget"
	scope := memory.ProjectRoot(proj)
	_ = memory.UpdateState(scope, func(st *memory.State) error {
		st.Budget = map[string]int64{time.Now().Format("2006-01-02"): 10_000_000}
		return nil
	})
	// budget exhausted → returns 0 without needing a model at all
	n, err := runPhase1(context.Background(), &config.Config{}, proj, true, func(string, ...any) {})
	if err != nil || n != 0 {
		t.Fatalf("budget gate failed: n=%d err=%v", n, err)
	}
}

func TestRunRespectsCooldownAndLock(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not installed")
	}
	setHome(t)
	proj := "/proj/cool"
	scope := memory.ProjectRoot(proj)
	cfg := &config.Config{}

	_ = memory.UpdateState(scope, func(st *memory.State) error {
		st.LastPipelineAt = time.Now().Format(time.RFC3339)
		return nil
	})
	// within cooldown → skip silently (no error), state unchanged
	var logs []string
	err := Run(context.Background(), cfg, proj, Options{Log: func(f string, a ...any) {
		logs = append(logs, fmt.Sprintf(f, a...))
	}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "cooldown") {
		t.Errorf("expected cooldown skip, logs: %s", joined)
	}

	// lock held → skip
	release, ok, err := memory.TryLockPipeline(scope)
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer release()
	logs = nil
	if err := Run(context.Background(), cfg, proj, Options{IgnoreCooldown: true, Log: func(f string, a ...any) {
		logs = append(logs, fmt.Sprintf(f, a...))
	}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "already running") {
		t.Errorf("expected lock skip, logs: %v", logs)
	}
}

func TestFirstJSONObject(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		// trailing prose containing a brace (the greedy-regex failure mode)
		{`{"summary":"s","slug":"x","memory":"- a"}` + "\n注:格式 {\"op\":1}", `{"summary":"s","slug":"x","memory":"- a"}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		// braces inside string literals must not confuse the scanner
		{`{"memory":"use {curly} braces"}`, `{"memory":"use {curly} braces"}`},
		{`no json here`, ``},
		{`{unbalanced`, ``},
	}
	for _, c := range cases {
		if got := firstJSONObject(c.in); got != c.want {
			t.Errorf("firstJSONObject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPhase2NoDiffAfterConsolidation(t *testing.T) {
	// Regression for the git-churn bug: after a real consolidation writes
	// MEMORY.md and state.json, a second phase2 must still take the no-op
	// fast path (state.json is gitignored). We simulate a consolidated scope
	// by hand-writing the artifacts, committing, then touching state.json.
	if !gitAvailable() {
		t.Skip("git not installed")
	}
	setHome(t)
	proj := "/proj/churn"
	scope := memory.ProjectRoot(proj)
	if err := memory.EnsureScope(scope); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureBaseline(scope); err != nil {
		t.Fatal(err)
	}
	// write curated artifacts + commit as a baseline
	if err := os.WriteFile(scope+"/MEMORY.md", []byte("# index\n- x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scope+"/memory_summary.md", []byte("v1\nsummary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := commitBaseline(scope, "test baseline"); err != nil {
		t.Fatal(err)
	}
	// Now churn state.json the way usage accounting + pipeline stamps do.
	for i := 0; i < 3; i++ {
		_ = memory.UpdateState(scope, func(st *memory.State) error {
			st.Files["MEMORY.md"] = &memory.FileUsage{UsageCount: i + 1}
			st.LastPipelineAt = "2026-07-04T00:00:0" + string(rune('0'+i)) + "Z"
			return nil
		})
	}
	// state.json churn must NOT make the workspace dirty.
	dirty, _, err := workspaceDirty(scope, 40000)
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		st, _ := runGit(scope, "status", "--porcelain")
		t.Fatalf("state.json churn made workspace dirty (no-op fast path broken):\n%s", st)
	}
}
