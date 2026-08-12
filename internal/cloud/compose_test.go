// compose_test.go covers the M12 compose facets: attachment sanitize/limits/
// landing, the ordered five-facet dispatch against a mock local control plane,
// and the capabilities mirror collection/reporting.
package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

// --- attachment unit tests ---

func TestSanitizeInboxName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":           "report.pdf",
		"../etc/passwd":        "passwd",
		"..\\..\\win.ini":      "win.ini",
		"a/b/c.txt":            "c.txt",
		".env":                 "env",
		"..":                   "attachment",
		".":                    "attachment",
		"":                     "attachment",
		"  ":                   "attachment",
		"a\x00b\x1fc.pdf":      "abc.pdf",
		"  spaced name.txt  ":  "spaced name.txt",
		".../.../...hidden.md": "hidden.md",
		"中文 附件.pdf":            "中文 附件.pdf",
	}
	for in, want := range cases {
		if got := sanitizeInboxName(in); got != want {
			t.Errorf("sanitizeInboxName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeAttachmentsLimits(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("hi"))

	// Count limit: 6 files breach the 5-file cap.
	tooMany := make([]chatAttachment, maxAttachmentCount+1)
	for i := range tooMany {
		tooMany[i] = chatAttachment{Name: fmt.Sprintf("f%d.txt", i), DataB64: b64}
	}
	if _, err := decodeAttachments(tooMany); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("6 attachments: err = %v, want count-limit error", err)
	}

	// Size limit: 2MB+1 decoded bytes breach the per-file cap.
	big := make([]byte, maxAttachmentBytes+1)
	_, err := decodeAttachments([]chatAttachment{{Name: "big.bin", DataB64: base64.StdEncoding.EncodeToString(big)}})
	if err == nil || !strings.Contains(err.Error(), "2MB") {
		t.Fatalf("oversize attachment: err = %v, want size-limit error", err)
	}

	// Invalid base64 is rejected.
	if _, err := decodeAttachments([]chatAttachment{{Name: "x", DataB64: "!!!not-b64!!!"}}); err == nil {
		t.Fatal("invalid base64: want error")
	}

	// Exactly at the limits passes: 5 files, one exactly 2MB.
	ok := []chatAttachment{
		{Name: "a", DataB64: base64.StdEncoding.EncodeToString(make([]byte, maxAttachmentBytes))},
		{Name: "b", DataB64: b64}, {Name: "c", DataB64: b64}, {Name: "d", DataB64: b64}, {Name: "e", DataB64: b64},
	}
	decoded, err := decodeAttachments(ok)
	if err != nil {
		t.Fatalf("at-limit attachments: %v", err)
	}
	if len(decoded) != 5 || len(decoded[0]) != maxAttachmentBytes {
		t.Fatalf("decoded = %d files (%d bytes), want 5 (%d)", len(decoded), len(decoded[0]), maxAttachmentBytes)
	}
}

func TestWriteInboxAttachments(t *testing.T) {
	root := t.TempDir()
	atts := []chatAttachment{
		{Name: "../../evil.txt"},
		{Name: "report.pdf"},
		{Name: "report.pdf"}, // collision → numeric suffix
	}
	decoded := [][]byte{[]byte("one"), []byte("two"), []byte("three")}

	refs, err := writeInboxAttachments(root, "sess-1", atts, decoded)
	if err != nil {
		t.Fatalf("writeInboxAttachments: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("refs = %v, want 3", refs)
	}

	// Traversal is flattened into the session dir; collision got "-2".
	wantNames := []string{"evil.txt", "report.pdf", "report-2.pdf"}
	for i, r := range refs {
		if r.Name != wantNames[i] {
			t.Errorf("refs[%d].Name = %q, want %q", i, r.Name, wantNames[i])
		}
		wantPath := filepath.Join(root, "sess-1", wantNames[i])
		if r.Path != wantPath {
			t.Errorf("refs[%d].Path = %q, want %q", i, r.Path, wantPath)
		}
		data, err := os.ReadFile(r.Path)
		if err != nil || string(data) != string(decoded[i]) {
			t.Errorf("refs[%d] content = %q, %v", i, data, err)
		}
		fi, err := os.Stat(r.Path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != inboxFileMode {
			t.Errorf("file perm = %o, want %o", perm, inboxFileMode)
		}
	}
	di, err := os.Stat(filepath.Join(root, "sess-1"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != inboxDirMode {
		t.Errorf("dir perm = %o, want %o", perm, inboxDirMode)
	}

	// The session id crosses the trust boundary too: sanitize it.
	if _, err := writeInboxAttachments(root, "../escape", atts[:1], decoded[:1]); err != nil {
		t.Fatalf("hostile session id: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape", "evil.txt")); err != nil {
		t.Errorf("hostile session id did not land inside the inbox root: %v", err)
	}
}

// --- compose dispatch tests ---

// fakeComposeLocal is a mock local control plane recording every call (in
// order) for the compose endpoints.
type fakeComposeLocal struct {
	mu        sync.Mutex
	calls     []string
	bodies    map[string][]map[string]any
	failPaths map[string]int // path → status to answer with

	sessionID      string
	healthProvider string
	healthModel    string
}

func newFakeComposeLocal(t *testing.T) (*fakeComposeLocal, *httptest.Server) {
	t.Helper()
	f := &fakeComposeLocal{
		bodies:         map[string][]map[string]any{},
		failPaths:      map[string]int{},
		sessionID:      "sess-compose-1",
		healthProvider: "prov-cur",
		healthModel:    "mod-cur",
	}
	record := func(path string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.calls = append(f.calls, path)
			f.bodies[path] = append(f.bodies[path], body)
			fail := f.failPaths[path]
			f.mu.Unlock()
			if fail != 0 {
				http.Error(w, "boom", fail)
				return
			}
			switch path {
			case "/api/sessions/activate":
				sessionID, _ := body["session_id"].(string)
				if sessionID == "" {
					sessionID = f.sessionID
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready", "session_id": sessionID})
			case "/api/chat":
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "processing", "session_id": f.sessionID})
			default:
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			}
		}
	}
	mux := http.NewServeMux()
	for _, p := range []string{"/api/sessions/activate", "/api/chat", "/api/model", "/api/mode", "/api/model-state/effort", "/api/goal"} {
		mux.HandleFunc("POST "+p, record(p))
	}
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, "GET /api/status")
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"provider": f.healthProvider, "model": f.healthModel})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeComposeLocal) snapshot() ([]string, map[string][]map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := append([]string(nil), f.calls...)
	bodies := map[string][]map[string]any{}
	for k, v := range f.bodies {
		bodies[k] = append([]map[string]any(nil), v...)
	}
	return calls, bodies
}

func TestChatSendComposeOrder(t *testing.T) {
	local, localSrv := newFakeComposeLocal(t)
	conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)
	conn.cfg.InboxDir = t.TempDir()

	cmd := DeviceCommand{
		ID:   "cmd-compose",
		Kind: "chat.send",
		Payload: mustPayload(t, map[string]any{
			"text":         "请阅读附件并总结",
			"channel":      "console",
			"mode":         "plan",
			"project_path": "/tmp/proj-a",
			"model":        map[string]string{"provider": "anthropic", "id": "claude-x"},
			"effort":       "high",
			"goal":         "交付 M12",
			"attachments": []map[string]string{
				{"name": "spec.pdf", "mime": "application/pdf", "data_b64": base64.StdEncoding.EncodeToString([]byte("pdf-bytes"))},
			},
		}),
	}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "ok" {
		t.Fatalf("status = %q, result = %v", status, result)
	}
	res, _ := result.(map[string]string)
	if res["session_id"] != local.sessionID {
		t.Fatalf("ack session_id = %v, want %q", result, local.sessionID)
	}

	calls, bodies := local.snapshot()
	wantOrder := []string{"/api/sessions/activate", "/api/model", "/api/model-state/effort", "/api/mode", "/api/goal", "/api/chat"}
	if strings.Join(calls, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("call order = %v, want %v", calls, wantOrder)
	}

	// Session create carries the project path.
	if bodies["/api/sessions/activate"][0]["project_path"] != "/tmp/proj-a" {
		t.Errorf("activation body = %v, want project_path /tmp/proj-a", bodies["/api/sessions/activate"][0])
	}
	// Model + effort (effort keyed by the command's model).
	if bodies["/api/model"][0]["provider"] != "anthropic" || bodies["/api/model"][0]["model"] != "claude-x" {
		t.Errorf("model body = %v", bodies["/api/model"][0])
	}
	eff := bodies["/api/model-state/effort"][0]
	if eff["provider"] != "anthropic" || eff["model"] != "claude-x" || eff["effort"] != "high" {
		t.Errorf("effort body = %v", eff)
	}
	// Goal with start=false (the chat message kicks the run off).
	goal := bodies["/api/goal"][0]
	if goal["objective"] != "交付 M12" || goal["start"] != false {
		t.Errorf("goal body = %v, want objective + start=false", goal)
	}
	// Chat: session id, channel passthrough, attachment reference appended.
	chat := bodies["/api/chat"][0]
	if chat["session_id"] != local.sessionID || chat["source"] != "console" {
		t.Errorf("chat body = %v", chat)
	}
	msg, _ := chat["message"].(string)
	wantRef := "[附件] spec.pdf → " + filepath.Join(conn.cfg.InboxDir, local.sessionID, "spec.pdf")
	if !strings.HasPrefix(msg, "请阅读附件并总结") || !strings.Contains(msg, wantRef) {
		t.Errorf("message = %q, want text + %q", msg, wantRef)
	}

	// The attachment landed on disk with the right content and permissions.
	data, err := os.ReadFile(filepath.Join(conn.cfg.InboxDir, local.sessionID, "spec.pdf"))
	if err != nil || string(data) != "pdf-bytes" {
		t.Errorf("inbox file = %q, %v", data, err)
	}
}

func TestChatSendComposeExistingSessionAndEffortFromHealth(t *testing.T) {
	local, localSrv := newFakeComposeLocal(t)
	conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)

	cmd := DeviceCommand{
		ID:        "cmd-effort",
		Kind:      "chat.send",
		SessionID: "sess-77",
		Payload: mustPayload(t, map[string]any{
			"text":   "think harder",
			"effort": "low",
		}),
	}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "ok" {
		t.Fatalf("status = %q, result = %v", status, result)
	}

	calls, bodies := local.snapshot()
	wantOrder := []string{"/api/sessions/activate", "GET /api/status", "/api/model-state/effort", "/api/chat"}
	if strings.Join(calls, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("call order = %v, want %v", calls, wantOrder)
	}
	// Focus passes the existing session id through.
	if bodies["/api/sessions/activate"][0]["session_id"] != "sess-77" {
		t.Errorf("activation body = %v, want session_id sess-77", bodies["/api/sessions/activate"][0])
	}
	// Effort without an explicit model resolves the task via /api/status.
	eff := bodies["/api/model-state/effort"][0]
	if eff["provider"] != "prov-cur" || eff["model"] != "mod-cur" || eff["effort"] != "low" {
		t.Errorf("effort body = %v, want current model from /api/status", eff)
	}
}

func TestChatSendComposeAttachmentsOnly(t *testing.T) {
	local, localSrv := newFakeComposeLocal(t)
	conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)
	conn.cfg.InboxDir = t.TempDir()

	cmd := DeviceCommand{
		ID:   "cmd-atts-only",
		Kind: "chat.send",
		Payload: mustPayload(t, map[string]any{
			"attachments": []map[string]string{
				{"name": "notes.txt", "data_b64": base64.StdEncoding.EncodeToString([]byte("n"))},
			},
		}),
	}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "ok" {
		t.Fatalf("status = %q, result = %v (attachments-only must be a valid message)", status, result)
	}
	_, bodies := local.snapshot()
	msg, _ := bodies["/api/chat"][0]["message"].(string)
	if !strings.Contains(msg, "[附件] notes.txt → ") {
		t.Errorf("message = %q, want the reference list as the text", msg)
	}
}

func TestChatSendAttachmentLimitBreachHasNoSideEffects(t *testing.T) {
	local, localSrv := newFakeComposeLocal(t)
	conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)
	conn.cfg.InboxDir = filepath.Join(t.TempDir(), "inbox")

	atts := make([]map[string]string, maxAttachmentCount+1)
	for i := range atts {
		atts[i] = map[string]string{"name": fmt.Sprintf("f%d", i), "data_b64": "aGk="}
	}
	cmd := DeviceCommand{
		ID:   "cmd-too-many",
		Kind: "chat.send",
		Payload: mustPayload(t, map[string]any{
			"text": "hi", "goal": "g", "attachments": atts,
		}),
	}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "error" {
		t.Fatalf("status = %q, want error", status)
	}
	if !strings.Contains(fmt.Sprint(result), "limit") {
		t.Fatalf("result = %v, want limit error", result)
	}
	calls, _ := local.snapshot()
	if len(calls) != 0 {
		t.Fatalf("local calls = %v, want none (no side effects on limit breach)", calls)
	}
	if _, err := os.Stat(conn.cfg.InboxDir); !os.IsNotExist(err) {
		t.Fatalf("inbox dir exists after rejected command")
	}
}

func TestChatSendComposeFacetErrorNamesField(t *testing.T) {
	local, localSrv := newFakeComposeLocal(t)
	local.failPaths["/api/model"] = http.StatusInternalServerError
	conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)

	cmd := DeviceCommand{
		ID:   "cmd-bad-model",
		Kind: "chat.send",
		Payload: mustPayload(t, map[string]any{
			"text": "hi", "model": map[string]string{"provider": "nope", "id": "nope"},
		}),
	}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "error" {
		t.Fatalf("status = %q, want error", status)
	}
	if !strings.Contains(fmt.Sprint(result), "model:") {
		t.Fatalf("result = %v, want the failing facet named", result)
	}
	// The goal/chat steps must not run after a model failure.
	calls, _ := local.snapshot()
	for _, c := range calls {
		if c == "/api/goal" || c == "/api/chat" {
			t.Fatalf("calls = %v, want pipeline aborted after the model failure", calls)
		}
	}
}

// --- capabilities tests ---

func TestSyncSessionsReportsCapabilities(t *testing.T) {
	cloud := newMockCloud()
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(t, cloudSrv.URL, "http://127.0.0.1:1")
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{
			"/proj-b": {{UUID: "s2", Status: "idle"}},
			"/proj-a": {{UUID: "s1", Status: "idle"}},
		}, nil
	}
	conn.cfg.ModelCapabilitiesFn = func() ([]CapabilityModel, []string, error) {
		return []CapabilityModel{{Provider: "anthropic", ID: "claude-x", Label: "Claude X"}}, []string{"low", "high"}, nil
	}

	if err := conn.syncSessions(context.Background()); err != nil {
		t.Fatalf("syncSessions: %v", err)
	}

	cloud.mu.Lock()
	caps := cloud.capsReqs
	cloud.mu.Unlock()
	if len(caps) != 1 || len(caps[0]) == 0 {
		t.Fatalf("capabilities payloads = %v, want one non-empty", caps)
	}
	var got DeviceCapabilities
	if err := json.Unmarshal(caps[0], &got); err != nil {
		t.Fatalf("capabilities JSON: %v", err)
	}
	// Projects come from the session index, sorted by path.
	if len(got.Projects) != 2 || got.Projects[0].Path != "/proj-a" || got.Projects[1].Path != "/proj-b" {
		t.Fatalf("projects = %+v, want /proj-a,/proj-b", got.Projects)
	}
	if got.Projects[0].Name != "proj-a" {
		t.Errorf("project name = %q, want proj-a (basename)", got.Projects[0].Name)
	}
	if len(got.Models) != 1 || got.Models[0].ID != "claude-x" || got.Models[0].Label != "Claude X" {
		t.Errorf("models = %+v", got.Models)
	}
	if strings.Join(got.Efforts, ",") != "low,high" {
		t.Errorf("efforts = %v", got.Efforts)
	}
}

func TestCollectModelCapabilities(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgJSON := `{
		"providers": {
			"my-gw": {
				"api_key": "k",
				"base_url": "http://gw",
				"custom_models": [
					{"id": "m-plain", "name": "Plain", "tool_call": true},
					{"id": "m-think", "name": "Thinker", "tool_call": true, "reasoning": true, "effort_tiers": ["low", "max"]}
				]
			}
		}
	}`
	if err := os.MkdirAll(filepath.Join(home, ".jcode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".jcode", "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	models, efforts, err := collectModelCapabilities()
	if err != nil {
		t.Fatalf("collectModelCapabilities: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v, want the two custom models", models)
	}
	byID := map[string]CapabilityModel{}
	for _, m := range models {
		if m.Provider != "my-gw" {
			t.Errorf("model provider = %q, want my-gw", m.Provider)
		}
		byID[m.ID] = m
	}
	if byID["m-think"].Label != "Thinker" {
		t.Errorf("m-think label = %q, want Thinker", byID["m-think"].Label)
	}
	// Efforts are the union of the models' reasoning options; only m-think
	// advertises any.
	if strings.Join(efforts, ",") != "low,max" {
		t.Errorf("efforts = %v, want [low max]", efforts)
	}
}

func TestCollectModelCapabilitiesFallbackEfforts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgJSON := `{
		"providers": {
			"my-gw": {
				"api_key": "k",
				"custom_models": [{"id": "m-plain", "tool_call": true}]
			}
		}
	}`
	if err := os.MkdirAll(filepath.Join(home, ".jcode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".jcode", "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	_, efforts, err := collectModelCapabilities()
	if err != nil {
		t.Fatalf("collectModelCapabilities: %v", err)
	}
	if strings.Join(efforts, ",") != "minimal,low,medium,high" {
		t.Errorf("efforts = %v, want the standard fallback set", efforts)
	}
}

// --- M14: slash_commands capability, goal_armed, images passthrough ---

func TestSyncSessionsReportsSlashCommands(t *testing.T) {
	cloud := newMockCloud()
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(t, cloudSrv.URL, "http://127.0.0.1:1")
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{"/proj": {{UUID: "s1", Status: "idle"}}}, nil
	}
	conn.cfg.ModelCapabilitiesFn = func() ([]CapabilityModel, []string, error) {
		return nil, nil, fmt.Errorf("models down") // best-effort: must not break the upsert
	}
	conn.cfg.SlashCommandsFn = func() ([]CapabilitySlashCommand, error) {
		return []CapabilitySlashCommand{
			{Slash: "/review", Description: "评审改动", Type: "skill"},
			{Slash: "/deploy", Description: "部署", Type: "flow"},
		}, nil
	}

	if err := conn.syncSessions(context.Background()); err != nil {
		t.Fatalf("syncSessions: %v", err)
	}

	cloud.mu.Lock()
	caps := cloud.capsReqs
	cloud.mu.Unlock()
	if len(caps) != 1 {
		t.Fatalf("capabilities payloads = %d, want 1", len(caps))
	}
	var got DeviceCapabilities
	if err := json.Unmarshal(caps[0], &got); err != nil {
		t.Fatalf("capabilities JSON: %v", err)
	}
	if len(got.SlashCommands) != 2 {
		t.Fatalf("slash_commands = %+v, want 2 entries", got.SlashCommands)
	}
	if got.SlashCommands[0].Slash != "/review" || got.SlashCommands[0].Type != "skill" ||
		got.SlashCommands[1].Slash != "/deploy" || got.SlashCommands[1].Type != "flow" {
		t.Errorf("slash_commands = %+v", got.SlashCommands)
	}
	// The failed model facet reported empty but did not fail the sync.
	if len(got.Models) != 0 || len(got.Efforts) != 0 {
		t.Errorf("models/efforts = %+v/%v, want empty on facet failure", got.Models, got.Efforts)
	}
}

// TestCollectSlashCommandsDefault exercises the default facet path against a
// fake local control plane: GET /api/slash-commands answers a bare JSON
// array; an unreachable server degrades to an empty list (never an error).
func TestCollectSlashCommandsDefault(t *testing.T) {
	localSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/slash-commands" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"slash": "/commit", "description": "提交", "type": "skill"},
		})
	}))
	t.Cleanup(localSrv.Close)

	conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)
	cmds, err := conn.collectSlashCommands(context.Background())
	if err != nil {
		t.Fatalf("collectSlashCommands: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Slash != "/commit" || cmds[0].Type != "skill" {
		t.Fatalf("slash commands = %+v", cmds)
	}

	// Unreachable control plane → error (the caller logs and reports empty).
	connDown := newTestConnector(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	if _, err := connDown.collectSlashCommands(context.Background()); err == nil {
		t.Fatal("unreachable control plane: want error")
	}
}

func TestChatSendGoalArmed(t *testing.T) {
	local, localSrv := newFakeComposeLocal(t)
	conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)

	cmd := DeviceCommand{
		ID:   "cmd-goal-armed",
		Kind: "chat.send",
		Payload: mustPayload(t, map[string]any{
			"text":       "交付 M14",
			"goal_armed": true,
			// Compose facets are ignored when goal_armed is set.
			"mode":   "plan",
			"goal":   "ignored",
			"images": []map[string]string{{"data": "aGk=", "media_type": "image/png"}},
		}),
	}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "ok" {
		t.Fatalf("status = %q, result = %v", status, result)
	}

	calls, bodies := local.snapshot()
	if strings.Join(calls, ",") != "/api/sessions/activate,/api/goal" {
		t.Fatalf("local calls = %v, want activation + /api/goal (no /api/chat, no compose steps)", calls)
	}
	goal := bodies["/api/goal"][0]
	if goal["objective"] != "交付 M14" || goal["start"] != true {
		t.Errorf("goal body = %v, want {objective, start:true}", goal)
	}

	// goal_armed with an empty objective is rejected without side effects.
	local2, localSrv2 := newFakeComposeLocal(t)
	conn2 := newTestConnector(t, "http://127.0.0.1:1", localSrv2.URL)
	empty := DeviceCommand{
		ID:      "cmd-goal-empty",
		Kind:    "chat.send",
		Payload: mustPayload(t, map[string]any{"text": "  ", "goal_armed": true}),
	}
	if status, result := conn2.executeCommand(context.Background(), empty); status != "error" {
		t.Fatalf("empty objective: status = %q, result = %v; want error", status, result)
	}
	if calls, _ := local2.snapshot(); len(calls) != 0 {
		t.Fatalf("empty objective: local calls = %v, want none", calls)
	}
}

func TestChatSendImagesForwarded(t *testing.T) {
	// Legacy path: images (incl. the optional name) reach /api/chat as-is.
	local, localSrv := newFakeLocal(t)
	conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)

	cmd := DeviceCommand{
		ID:   "cmd-imgs",
		Kind: "chat.send",
		Payload: mustPayload(t, map[string]any{
			"text": "看图",
			"images": []map[string]string{
				{"data": "aGk=", "media_type": "image/png", "name": "shot.png"},
				{"data": "aGky", "media_type": "image/jpeg"},
			},
		}),
	}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "ok" {
		t.Fatalf("status = %q, result = %v", status, result)
	}
	imgs, ok := local.chatBody()["images"].([]any)
	if !ok || len(imgs) != 2 {
		t.Fatalf("images = %v, want 2 entries", local.chatBody()["images"])
	}
	first, _ := imgs[0].(map[string]any)
	if first["data"] != "aGk=" || first["media_type"] != "image/png" || first["name"] != "shot.png" {
		t.Errorf("images[0] = %v, want data/media_type/name preserved", first)
	}
	second, _ := imgs[1].(map[string]any)
	if second["media_type"] != "image/jpeg" {
		t.Errorf("images[1] = %v", second)
	}
	if _, has := second["name"]; has {
		t.Errorf("images[1] = %v, want name omitted when empty", second)
	}

	// Compose path: images ride the same /api/chat call.
	compLocal, compSrv := newFakeComposeLocal(t)
	conn2 := newTestConnector(t, "http://127.0.0.1:1", compSrv.URL)
	cmd2 := DeviceCommand{
		ID:   "cmd-imgs-compose",
		Kind: "chat.send",
		Payload: mustPayload(t, map[string]any{
			"text":         "看图",
			"project_path": "/tmp/proj",
			"images":       []map[string]string{{"data": "aGk=", "media_type": "image/png", "name": "shot.png"}},
		}),
	}
	status, result = conn2.executeCommand(context.Background(), cmd2)
	if status != "ok" {
		t.Fatalf("compose: status = %q, result = %v", status, result)
	}
	_, bodies := compLocal.snapshot()
	cimgs, ok := bodies["/api/chat"][0]["images"].([]any)
	if !ok || len(cimgs) != 1 {
		t.Fatalf("compose chat images = %v, want 1 entry", bodies["/api/chat"][0])
	}
	cimg, _ := cimgs[0].(map[string]any)
	if cimg["name"] != "shot.png" || cimg["media_type"] != "image/png" {
		t.Errorf("compose images[0] = %v", cimg)
	}
}
