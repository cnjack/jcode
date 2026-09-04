package web

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	internalagent "github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/session"
)

type conversationAutomationModel struct {
	mu      sync.Mutex
	history []*schema.Message
}

func (m *conversationAutomationModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *conversationAutomationModel) Generate(
	_ context.Context,
	messages []*schema.Message,
	_ ...einomodel.Option,
) (*schema.Message, error) {
	m.capture(messages)
	return schema.AssistantMessage("automation complete", nil), nil
}

func (m *conversationAutomationModel) Stream(
	_ context.Context,
	messages []*schema.Message,
	_ ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	m.capture(messages)
	return schema.StreamReaderFromArray([]*schema.Message{
		schema.AssistantMessage("automation complete", nil),
	}), nil
}

func (m *conversationAutomationModel) capture(messages []*schema.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append([]*schema.Message(nil), messages...)
}

func (m *conversationAutomationModel) snapshot() []*schema.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*schema.Message(nil), m.history...)
}

// runAutomation keys the engine on eng.taskID (the single registration done by
// buildLocalEngine) and reclaims it with deleteEngine(eng.taskID). This guards
// the Finding-1 contract: the engine must live under exactly ONE tasks-map key
// so a run can't leak an entry (the earlier code registered a second time under
// a different id, leaking one entry per run and exhausting the pool after
// maxLiveEngines runs).
func TestAutomationEngineRegisteredOnceAndReclaimed(t *testing.T) {
	s := stubFactoryServer(t)

	for i := 0; i < maxLiveEngines+8; i++ {
		eng, err := s.buildLocalEngine("", "/proj/auto", "full_access")
		if err != nil {
			t.Fatalf("run %d: buildLocalEngine: %v (engine pool leaked?)", i, err)
		}
		sid := eng.taskID // exactly what runAutomation uses as the session id

		s.tasksMu.RLock()
		n := len(s.tasks)
		_, ok := s.tasks[sid]
		s.tasksMu.RUnlock()
		if !ok {
			t.Fatalf("run %d: engine not registered under its taskID", i)
		}
		if n != 1 {
			t.Fatalf("run %d: want exactly 1 live engine, got %d (double registration leaks)", i, n)
		}

		s.deleteEngine(sid) // run completion
		s.tasksMu.RLock()
		n = len(s.tasks)
		s.tasksMu.RUnlock()
		if n != 0 {
			t.Fatalf("run %d: engine not reclaimed, %d still live", i, n)
		}
	}
}

func TestBootstrapEngineCarriesConversationAutomationBuilder(t *testing.T) {
	build := func() (*adk.ChatModelAgent, error) { return nil, nil }
	s := NewServer(&ServerConfig{RebuildForAutomation: build})
	if s.Engine == nil || s.rebuildForAutomation == nil {
		t.Fatal("bootstrap engine lost RebuildForAutomation")
	}
}

func TestAutomationRunModeForcesUnattendedTriggersToFullAccess(t *testing.T) {
	tests := []struct {
		name    string
		trigger automation.TriggerType
		mode    string
		want    string
	}{
		{name: "schedule", trigger: automation.TriggerSchedule, mode: "approval", want: "full_access"},
		{name: "once", trigger: automation.TriggerOnce, mode: "plan", want: "full_access"},
		{name: "manual", trigger: automation.TriggerManual, mode: "approval", want: "approval"},
		{name: "empty fallback", trigger: automation.TriggerManual, mode: "", want: "full_access"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &automation.Automation{Mode: tt.mode, Trigger: automation.Trigger{Type: tt.trigger}}
			if got := automationRunMode(a); got != tt.want {
				t.Fatalf("automationRunMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConversationAutomationContinuesOwnerHistory(t *testing.T) {
	s := stubFactoryServer(t)
	project := t.TempDir()
	eng, err := s.buildLocalEngine("owner-session", project, "approval")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.teardown)
	eng.recorder.RecordUser("earlier request")
	eng.emu.Lock()
	eng.history = []adk.Message{
		schema.UserMessage("earlier request"),
		schema.AssistantMessage("earlier answer", nil),
	}
	eng.eventHandler = eng.handler
	eng.emu.Unlock()

	model := &conversationAutomationModel{}
	ag, err := internalagent.NewAgent(context.Background(), model, nil, "system", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng.emu.Lock()
	eng.agent = ag
	eng.emu.Unlock()
	eng.rebuildForAutomation = func() (*adk.ChatModelAgent, error) { return ag, nil }

	a := &automation.Automation{
		ID: "automation-1", Name: "Continue", Prompt: "check CPU now", ProjectPath: project,
		ContextPolicy: automation.ContextConversation, OwnerSessionID: "owner-session",
		Trigger: automation.Trigger{Type: automation.TriggerManual}, Enabled: true,
	}
	sid, err := s.runAutomation(context.Background(), a, automation.KindManual)
	if err != nil {
		t.Fatal(err)
	}
	if sid != "owner-session" {
		t.Fatalf("run session=%q, want owner-session", sid)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		meta, _ := session.FindSessionMeta("owner-session")
		if !eng.running.Load() && meta != nil && meta.Status == "idle" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	meta, _ := session.FindSessionMeta("owner-session")
	if eng.running.Load() || meta == nil || meta.Status != "idle" {
		t.Fatal("conversation automation engine did not return to idle")
	}
	history := model.snapshot()
	var sawEarlierRequest, sawEarlierAnswer bool
	for _, message := range history {
		sawEarlierRequest = sawEarlierRequest || message.Content == "earlier request"
		sawEarlierAnswer = sawEarlierAnswer || message.Content == "earlier answer"
	}
	if !sawEarlierRequest || !sawEarlierAnswer {
		t.Fatalf("owner history not preserved: %+v", history)
	}
	last := history[len(history)-1]
	if last.Role != schema.User || !strings.Contains(last.Content, "<automation-fire") || !strings.Contains(last.Content, "check CPU now") {
		t.Fatalf("automation fire was not injected as the latest user message: %+v", last)
	}
}
