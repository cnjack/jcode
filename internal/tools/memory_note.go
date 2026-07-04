package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	appconfig "github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory"
)

// MemoryNoteDeps wires session identity into the memory_note tool.
type MemoryNoteDeps struct {
	// SessionIDFn returns the current session UUID for note provenance. May be nil.
	SessionIDFn func() string
}

type MemoryNoteInput struct {
	Scope  string `json:"scope,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Source string `json:"source,omitempty"`
	Text   string `json:"text"`
}

// NewMemoryNoteTool creates the L1 online-note tool. Notes go to the memory
// inbox only; curated memory files are maintained by the offline pipeline.
// Write scope is locked to the memory root by the implementation (path guard
// in internal/memory), not by prompt discipline.
func (e *Env) NewMemoryNoteTool(deps *MemoryNoteDeps) tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "memory_note",
		Desc: `Save one durable fact to persistent cross-session memory (the project's memory inbox).

WHEN TO USE:
- The user explicitly asks to remember/save something for the future ("remember X", "记住X") — you MUST call this tool then, with source="user".
- You learned a durable fact, preference, pitfall, or workflow in this session that would change default behavior in FUTURE sessions (set source="agent").

WHEN NOT TO USE (write discipline):
- Facts derivable from the repo itself (code structure, git history, AGENTS.md content).
- Details that only matter for the current session.
- Routine task progress — use the todo tools for that.

One fact per call. Secrets are redacted automatically; do not store credentials.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"text": {
				Type:     schema.String,
				Desc:     "The fact to remember, phrased so it is useful without this session's context.",
				Required: true,
			},
			"scope": {
				Type: schema.String,
				Desc: "\"project\" (default) for facts about this project; \"global\" for user-level preferences that apply everywhere.",
				Enum: []string{"project", "global"},
			},
			"kind": {
				Type: schema.String,
				Desc: "preference | fact | pitfall | workflow (default fact)",
				Enum: []string{"preference", "fact", "pitfall", "workflow"},
			},
			"source": {
				Type: schema.String,
				Desc: "\"user\" when the user explicitly asked to remember this; \"agent\" (default) when you decided to record it.",
				Enum: []string{"user", "agent"},
			},
		}),
	}
	return &memoryNoteTool{env: e, deps: deps, info: info}
}

type memoryNoteTool struct {
	env  *Env
	deps *MemoryNoteDeps
	info *schema.ToolInfo
}

func (t *memoryNoteTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *memoryNoteTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	cfg, _ := appconfig.LoadConfig()
	if !appconfig.MemoryEnabled(cfg) {
		return "", fmt.Errorf("memory is disabled (memory.enabled=false); nothing was saved")
	}
	var input MemoryNoteInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}
	sessionID := ""
	if t.deps != nil && t.deps.SessionIDFn != nil {
		sessionID = t.deps.SessionIDFn()
	}
	path, err := memory.WriteNote(memory.Note{
		Scope:     input.Scope,
		Kind:      input.Kind,
		Source:    input.Source,
		Text:      input.Text,
		SessionID: sessionID,
		Cwd:       t.env.Pwd(),
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Saved to memory inbox: %s\nIt will be consolidated into the project's curated memory by the background pipeline.", path), nil
}
