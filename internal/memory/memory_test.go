package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/cnjack/jcode/internal/config"
)

// setHome points config.ConfigDir()'s HOME at a temp dir for the test.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows
	return home
}

func TestRedact(t *testing.T) {
	cases := []struct {
		in       string
		mustLose []string // substrings that must not survive
		mustKeep []string
	}{
		{"key is sk-test-51Habc123FAKEKEY999 ok", []string{"sk-test-51Habc123FAKEKEY999"}, []string{"key is", "ok"}},
		{"ghp_abcdefghijklmnop123456 and ghs_ABCDEFGHIJKLMNOP1234", []string{"ghp_", "ghs_"}, nil},
		{"aws AKIAIOSFODNN7EXAMPLE done", []string{"AKIAIOSFODNN7EXAMPLE"}, []string{"aws", "done"}},
		{"url postgres://user:hunter2@db.example.com/x", []string{"hunter2"}, []string{"postgres://user"}},
		{"Authorization: Bearer abcdef1234567890abcdef", []string{"abcdef1234567890abcdef"}, nil},
		{"api_key = \"supersecretvalue\" rest", []string{"supersecretvalue"}, []string{"api_key", "rest"}},
		{"password: topsecret99", []string{"topsecret99"}, []string{"password"}},
		{"slack xoxb-1234567890-abcdef", []string{"xoxb-1234567890"}, nil},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----", []string{"MIIE"}, nil},
		// no false positives on prose
		{"the token budget is 300k and make test-fast is preferred", nil, []string{"token budget", "make test-fast"}},
		// review-found gaps now covered:
		{`{"api_key": "sk_live_ABCDEFGH12345678"}`, []string{"sk_live_ABCDEFGH12345678"}, []string{"api_key"}},
		{`config {"password":"myp@ss/word:1"}`, []string{"myp@ss/word"}, []string{"password"}},
		{"redis://admin:p/a:ss@10.0.0.1:6379", []string{"p/a:ss"}, []string{"redis://admin"}},
		{"github_pat_11ABCDEFG0abcdefghij_KLMNOPqrstuvwxyz123456", []string{"github_pat_11ABCDEFG0"}, nil},
		{"export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLE", []string{"wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLE"}, []string{"AWS_SECRET_ACCESS_KEY"}},
	}
	for _, c := range cases {
		got := Redact(c.in)
		for _, bad := range c.mustLose {
			if strings.Contains(got, bad) {
				t.Errorf("Redact(%q) = %q; still contains %q", c.in, got, bad)
			}
		}
		for _, keep := range c.mustKeep {
			if !strings.Contains(got, keep) {
				t.Errorf("Redact(%q) = %q; lost %q", c.in, got, keep)
			}
		}
		// idempotent
		if again := Redact(got); again != got {
			t.Errorf("Redact not idempotent: %q -> %q", got, again)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	// pure ASCII: byte cut
	if got := TruncateRunes("hello world", 5, "…"); got != "hello…" {
		t.Errorf("ascii: %q", got)
	}
	// no truncation when under limit
	if got := TruncateRunes("hi", 10, "…"); got != "hi" {
		t.Errorf("under: %q", got)
	}
	// Chinese: cut must land on a rune boundary → result stays valid UTF-8
	zh := "部署命令是脚本" // 7 runes x 3 bytes = 21 bytes
	for _, max := range []int{4, 5, 7, 10, 13, 20} {
		got := TruncateRunes(zh, max, "…")
		if !utf8.ValidString(got) {
			t.Errorf("TruncateRunes(zh, %d) produced invalid UTF-8: %q", max, got)
		}
		if len(got) > max+len("…") {
			t.Errorf("TruncateRunes(zh, %d) too long: %d bytes", max, len(got))
		}
	}
}

func TestProjectSlug(t *testing.T) {
	setHome(t)
	a := ProjectSlug("/tmp/some/proj")
	b := ProjectSlug("/tmp/other/proj")
	if a == b {
		t.Fatalf("same-named projects must get distinct slugs: %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "proj-") || len(a) != len("proj-")+8 {
		t.Errorf("unexpected slug shape: %s", a)
	}
	// stability
	if a != ProjectSlug("/tmp/some/proj") {
		t.Error("slug not stable")
	}
	// hostile characters sanitized
	weird := ProjectSlug("/tmp/we ird/pro j@#$%")
	if strings.ContainsAny(weird, " @#$%") {
		t.Errorf("slug not sanitized: %s", weird)
	}
	// 中文路径不 panic 且非空
	zh := ProjectSlug("/tmp/项目/中文目录")
	if zh == "" {
		t.Error("empty slug for chinese path")
	}
}

func TestWithinRootGuard(t *testing.T) {
	home := setHome(t)
	root := filepath.Join(home, ".jcode", "memory")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	ok := []string{
		filepath.Join(root, "projects", "x", "notes", "a.md"),
		filepath.Join(root, "global", "MEMORY.md"),
	}
	for _, p := range ok {
		if err := withinRoot(root, p); err != nil {
			t.Errorf("withinRoot rejected legit path %s: %v", p, err)
		}
	}
	bad := []string{
		filepath.Join(root, "..", "config.json"),
		filepath.Join(root, "projects", "..", "..", "config.json"),
		"/etc/passwd",
		filepath.Join(root, "projects", "%2e%2e", "x"),
		filepath.Join(root, "notes", "%2E%2E%2Fescape"),
	}
	for _, p := range bad {
		if err := withinRoot(root, p); err == nil {
			t.Errorf("withinRoot allowed escape path %s", p)
		}
	}
	// symlink escape: root/projects/link -> home (outside root)
	link := filepath.Join(root, "projects", "link")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(home, link); err == nil {
		if err := withinRoot(root, filepath.Join(link, "escaped.md")); err == nil {
			t.Error("withinRoot allowed symlink escape")
		}
	}
}

func TestWriteNoteAndRecentNotes(t *testing.T) {
	setHome(t)
	proj := t.TempDir()
	p, err := WriteNote(Note{
		Scope: "project", Kind: "preference", Source: "user",
		Text: "run tests with make test-fast; api_key = verysecret123", SessionID: "s-1", Cwd: proj,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"kind: preference", "source: user", "session: s-1", "make test-fast"} {
		if !strings.Contains(s, want) {
			t.Errorf("note missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "verysecret123") {
		t.Error("note not redacted")
	}
	if !strings.HasPrefix(p, ProjectRoot(proj)) {
		t.Errorf("note landed outside project root: %s", p)
	}

	// empty & oversized rejected
	if _, err := WriteNote(Note{Text: "   ", Cwd: proj}); err == nil {
		t.Error("empty note accepted")
	}
	if _, err := WriteNote(Note{Text: strings.Repeat("x", MaxNoteBytes+1), Cwd: proj}); err == nil {
		t.Error("oversized note accepted")
	}

	// second note, then RecentNotes order (newest first)
	if _, err := WriteNote(Note{Text: "zzz newest note", Cwd: proj}); err != nil {
		t.Fatal(err)
	}
	notes := RecentNotes(ProjectRoot(proj), 10)
	if len(notes) != 2 {
		t.Fatalf("want 2 notes, got %d", len(notes))
	}
	if !strings.Contains(notes[0].Text, "zzz newest") {
		t.Errorf("notes not newest-first: %+v", notes[0])
	}
	if notes[1].Kind != "preference" || notes[1].Source != "user" {
		t.Errorf("frontmatter not parsed: %+v", notes[1])
	}
}

func TestNoteSlugCJKAndConcurrency(t *testing.T) {
	// Chinese text must not collapse to a fixed "note" slug.
	s1 := noteSlug("记住我们用 make test-fast 运行测试")
	s2 := noteSlug("部署走 canary 流程")
	if s1 == "note" || s2 == "note" || s1 == s2 {
		t.Errorf("CJK slugs collapsed: %q %q", s1, s2)
	}
	// empty-after-strip falls back to a hash, not a fixed constant.
	if got := noteSlug("///***"); !strings.HasPrefix(got, "note-") || got == "note" {
		t.Errorf("fallback slug: %q", got)
	}

	// Concurrent same-second writes must not lose notes (O_EXCL claim).
	setHome(t)
	proj := t.TempDir()
	var wg sync.WaitGroup
	const n = 12
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := WriteNote(Note{Text: fmt.Sprintf("并发笔记编号 %d unique-%d", i, i), Cwd: proj})
			if err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	notes := RecentNotes(ProjectRoot(proj), 100)
	if len(notes) != n {
		t.Fatalf("concurrent writes lost notes: want %d, got %d", n, len(notes))
	}
	// each note's unique marker must be present exactly once
	seen := map[string]int{}
	for _, nf := range notes {
		for i := 0; i < n; i++ {
			if strings.Contains(nf.Text, fmt.Sprintf("unique-%d", i)) {
				seen[fmt.Sprintf("unique-%d", i)]++
			}
		}
	}
	if len(seen) != n {
		t.Errorf("expected %d distinct notes, got %d: %v", n, len(seen), seen)
	}
}

func TestClearScope(t *testing.T) {
	setHome(t)
	proj := t.TempDir()
	scope := ProjectRoot(proj)
	// seed some content
	if _, err := WriteNote(Note{Text: "keep me until cleared", Cwd: proj}); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(scope, NotesDir)) {
		t.Fatal("scope not created")
	}

	// busy: a held pipeline lock makes clear refuse without deleting.
	release, ok, err := TryLockPipeline(scope)
	if err != nil || !ok {
		t.Fatalf("could not take lock: ok=%v err=%v", ok, err)
	}
	busy, cerr := ClearScope(scope)
	if !busy || cerr != nil {
		t.Errorf("expected busy=true err=nil while lock held, got busy=%v err=%v", busy, cerr)
	}
	if !fileExists(scope) {
		t.Error("scope was deleted despite pipeline lock being held")
	}
	release()

	// not busy: clear wipes the scope.
	busy, cerr = ClearScope(scope)
	if busy || cerr != nil {
		t.Fatalf("expected clean clear, got busy=%v err=%v", busy, cerr)
	}
	if fileExists(scope) {
		t.Error("scope still exists after ClearScope")
	}

	// clearing a non-existent scope is a no-op success.
	if busy, cerr := ClearScope(scope); busy || cerr != nil {
		t.Errorf("clearing missing scope should succeed, got busy=%v err=%v", busy, cerr)
	}
}

func TestStateConcurrentUpdates(t *testing.T) {
	setHome(t)
	scope := filepath.Join(Root(), "projects", "t-00000000")
	var wg sync.WaitGroup
	const n = 20
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = UpdateState(scope, func(st *State) error {
				u := st.Files["MEMORY.md"]
				if u == nil {
					u = &FileUsage{}
					st.Files["MEMORY.md"] = u
				}
				u.UsageCount++
				return nil
			})
		}()
	}
	wg.Wait()
	st := LoadState(scope)
	if got := st.Files["MEMORY.md"].UsageCount; got != n {
		t.Errorf("lost updates: want %d, got %d", n, got)
	}
}

func TestStateCorruptSelfHeal(t *testing.T) {
	setHome(t)
	scope := filepath.Join(Root(), "projects", "c-00000000")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath(scope), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := LoadState(scope) // must not panic
	if st.Files == nil {
		t.Error("corrupt state not healed")
	}
	if err := UpdateState(scope, func(st *State) error { st.Files["x"] = &FileUsage{UsageCount: 1}; return nil }); err != nil {
		t.Fatalf("UpdateState over corrupt file: %v", err)
	}
}

func TestRecordUsageAndMiddlewareParsing(t *testing.T) {
	setHome(t)
	proj := t.TempDir()
	root := ProjectRoot(proj)
	if err := EnsureScope(root); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "MEMORY.md")

	// direct hit via file_path key
	args, _ := json.Marshal(map[string]any{"file_path": target})
	recordArgsUsage(string(args))
	// command token hit
	args2, _ := json.Marshal(map[string]any{"command": "grep -n foo " + target})
	recordArgsUsage(string(args2))
	// non-memory path: no accounting
	args3, _ := json.Marshal(map[string]any{"file_path": filepath.Join(proj, "main.go")})
	recordArgsUsage(string(args3))

	st := LoadState(root)
	u := st.Files["MEMORY.md"]
	if u == nil || u.UsageCount != 2 {
		t.Fatalf("usage accounting wrong: %+v", st.Files)
	}
	if len(st.Files) != 1 {
		t.Errorf("unexpected extra tracked files: %+v", st.Files)
	}
	// state.json itself never tracked
	argsState, _ := json.Marshal(map[string]any{"file_path": filepath.Join(root, StateFile)})
	recordArgsUsage(string(argsState))
	if st := LoadState(root); st.Files[StateFile] != nil {
		t.Error("state.json should not be usage-tracked")
	}
}

func TestBuildInjection(t *testing.T) {
	setHome(t)
	proj := t.TempDir()
	cfg := &config.Config{}

	// nothing → empty
	if got := BuildInjection(proj, cfg); got != "" {
		t.Errorf("expected empty injection, got %q", got)
	}

	// summary present → injected & truncated
	root := ProjectRoot(proj)
	if err := EnsureScope(root); err != nil {
		t.Fatal(err)
	}
	long := "v1\n" + strings.Repeat("deploy with ./scripts/deploy.sh --canary\n", 400)
	if err := os.WriteFile(filepath.Join(root, SummaryFile), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	got := BuildInjection(proj, cfg)
	if !strings.Contains(got, "--canary") || !strings.Contains(got, "Project Memory") {
		t.Errorf("summary not injected: %.200s", got)
	}
	if len(got) > config.MemorySummaryInjectTokens(cfg)*4+2500 {
		t.Errorf("injection not truncated: %d chars", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Error("truncation marker missing")
	}

	// notes injected
	if _, err := WriteNote(Note{Text: "sign-off phrase is NIGHTOWL-42", Source: "user", Cwd: proj}); err != nil {
		t.Fatal(err)
	}
	got = BuildInjection(proj, cfg)
	if !strings.Contains(got, "NIGHTOWL-42") {
		t.Error("recent note not injected")
	}

	// disabled → empty
	off := false
	cfgOff := &config.Config{Memory: &config.MemoryConfig{Enabled: &off}}
	if got := BuildInjection(proj, cfgOff); got != "" {
		t.Error("disabled memory still injected")
	}
}
