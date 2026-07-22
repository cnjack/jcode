package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
)

// newMockOpenAI returns an OpenAI-compatible /chat/completions server that
// records the "model" field of every request body — the ground truth for
// model-routing assertions — and replies with a single plain assistant
// message so agent loops terminate after one turn.
func newMockOpenAI(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		mu.Lock()
		models = append(models, body.Model)
		mu.Unlock()
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(
				`{"id":"1","object":"chat.completion.chunk","created":1,"model":%q,"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
				body.Model))
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w,
			`{"id":"1","object":"chat.completion","created":1,"model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			body.Model)
	}))
	capture := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), models...)
	}
	return srv, capture
}

// routingFixture builds a subagent tool wired the way production surfaces are
// (interactive.go / web.go): parent ChatModel + a ModelFactory whose fallback
// is the parent model.
func routingFixture(t *testing.T, srvURL, smallModel string) *subagentTool {
	t.Helper()
	pc := &config.ProviderConfig{APIKey: "test-key", BaseURL: srvURL}
	cfg := &config.Config{
		Model:      "mock/main-model",
		SmallModel: smallModel,
		Providers:  map[string]*config.ProviderConfig{"mock": pc},
	}
	parent, err := internalmodel.NewChatModelFromProvider(context.Background(), "mock", "main-model", srvURL, pc)
	if err != nil {
		t.Fatalf("build parent model: %v", err)
	}
	factory := internalmodel.NewModelFactory(cfg, parent)
	env := NewEnv(t.TempDir(), "linux")
	st, ok := env.NewSubagentTool(&SubagentDeps{ChatModel: parent, ModelFactory: factory}).(*subagentTool)
	if !ok {
		t.Fatal("unexpected subagent tool type")
	}
	return st
}

// TestSubagentModelRouting_SmallAlias drives the full in-process stack —
// subagent tool → ModelFactory alias resolution → chat model → HTTP request —
// and asserts the wire-level "model" field, i.e. that model:"small" really
// runs the subagent on the configured small model.
func TestSubagentModelRouting_SmallAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, captured := newMockOpenAI(t)
	defer srv.Close()

	st := routingFixture(t, srv.URL, "mock/small-model")
	out, err := st.InvokableRun(context.Background(),
		`{"name":"probe","description":"d","prompt":"reply done","agent_type":"explore","model":"small"}`)
	if err != nil {
		t.Fatalf("subagent run failed: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Errorf("subagent result lost: %q", out)
	}

	models := captured()
	if len(models) == 0 {
		t.Fatal("no chat-completion requests captured")
	}
	for _, m := range models {
		if m != "small-model" {
			t.Errorf("subagent request went to %q, want small-model (all: %v)", m, models)
		}
	}
}

// TestSubagentModelRouting_SmallAliasUnset locks the graceful-degradation
// contract: model:"small" without a configured small_model must run on the
// parent model and still complete — never error.
func TestSubagentModelRouting_SmallAliasUnset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, captured := newMockOpenAI(t)
	defer srv.Close()

	st := routingFixture(t, srv.URL, "")
	out, err := st.InvokableRun(context.Background(),
		`{"name":"probe","description":"d","prompt":"reply done","agent_type":"explore","model":"small"}`)
	if err != nil {
		t.Fatalf("subagent run failed: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Errorf("subagent result lost: %q", out)
	}

	for _, m := range captured() {
		if m != "main-model" {
			t.Errorf("unset alias must degrade to parent model, got %q", m)
		}
	}
}

// TestSubagentModelRouting_InheritsParent guards the default path: no model
// override → parent model, byte-identical behavior to before the alias existed.
func TestSubagentModelRouting_InheritsParent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, captured := newMockOpenAI(t)
	defer srv.Close()

	st := routingFixture(t, srv.URL, "mock/small-model")
	out, err := st.InvokableRun(context.Background(),
		`{"name":"probe","description":"d","prompt":"reply done","agent_type":"explore"}`)
	if err != nil {
		t.Fatalf("subagent run failed: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Errorf("subagent result lost: %q", out)
	}

	models := captured()
	if len(models) == 0 {
		t.Fatal("no chat-completion requests captured")
	}
	for _, m := range models {
		if m != "main-model" {
			t.Errorf("default subagent must inherit parent model, got %q", m)
		}
	}
}

// TestSubagentModelRouting_ExplicitRef covers the pre-existing documented
// contract (explicit 'provider/model' override) that was silently broken in
// production until ModelFactory was wired into SubagentDeps.
func TestSubagentModelRouting_ExplicitRef(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, captured := newMockOpenAI(t)
	defer srv.Close()

	st := routingFixture(t, srv.URL, "")
	_, err := st.InvokableRun(context.Background(),
		`{"name":"probe","description":"d","prompt":"reply done","agent_type":"explore","model":"mock/other-model"}`)
	if err != nil {
		t.Fatalf("subagent run failed: %v", err)
	}
	models := captured()
	if len(models) == 0 {
		t.Fatal("no chat-completion requests captured")
	}
	for _, m := range models {
		if m != "other-model" {
			t.Errorf("explicit override ignored, request went to %q", m)
		}
	}
}

func TestSubagentCustomRoleModelDefaultAndExplicitOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, captured := newMockOpenAI(t)
	defer srv.Close()

	st := routingFixture(t, srv.URL, "mock/small-model")
	st.deps.AgentRoles = map[string]config.AgentRoleConfig{
		"reviewer": {Description: "review", Profile: "explore", Instructions: "review carefully", Model: "small"},
	}
	if _, err := st.InvokableRun(context.Background(),
		`{"name":"role-default","description":"d","prompt":"done","agent_type":"reviewer"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InvokableRun(context.Background(),
		`{"name":"explicit","description":"d","prompt":"done","agent_type":"reviewer","model":"mock/explicit"}`); err != nil {
		t.Fatal(err)
	}
	models := captured()
	if len(models) < 2 || models[0] != "small-model" || models[len(models)-1] != "explicit" {
		t.Fatalf("custom role model routing = %v", models)
	}
}
