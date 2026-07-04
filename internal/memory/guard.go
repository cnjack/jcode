package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

type ctxKey int

const (
	ctxKeyNoAccounting ctxKey = iota
)

// WithoutUsageAccounting marks a context so the usage middleware ignores tool
// calls made under it. The consolidation agent reads every memory file each
// run; letting that count as "usage" would distort the ranking signal.
func WithoutUsageAccounting(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyNoAccounting, true)
}

func accountingDisabled(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeyNoAccounting).(bool)
	return v
}

// NewPathGuardMiddleware confines every tool call to root: any path-bearing
// argument that resolves outside root is rejected before the tool runs. This
// is the implementation-level containment for the consolidation subagent —
// it does not rely on the prompt.
func NewPathGuardMiddleware(root string) adk.ChatModelAgentMiddleware {
	return &pathGuardMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		root:                         root,
	}
}

type pathGuardMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	root string
}

func (m *pathGuardMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		if err := m.checkArgs(argumentsInJSON); err != nil {
			// Agent-visible refusal, not a loop-aborting error.
			return fmt.Sprintf("Path guard: %v. You may only touch files under %s.", err, m.root), nil
		}
		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

func (m *pathGuardMiddleware) checkArgs(argumentsInJSON string) error {
	var args map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return nil // let the tool produce its own parse error
	}
	for k, v := range args {
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		if k == "command" {
			return fmt.Errorf("shell commands are not allowed here")
		}
		if !pathKeys[k] {
			continue
		}
		p := s
		if !filepath.IsAbs(p) {
			p = filepath.Join(m.root, p)
		}
		if err := withinRoot(m.root, p); err != nil {
			return fmt.Errorf("%q escapes the memory workspace", s)
		}
		// Never let the agent write into the git dir: a planted hook
		// (.git/hooks/pre-commit) would execute when the pipeline commits —
		// a real escalation path given memory content is treated as data.
		clean := filepath.Clean(p)
		gitDir := filepath.Join(filepath.Clean(m.root), ".git")
		if clean == gitDir || strings.HasPrefix(clean, gitDir+string(filepath.Separator)) {
			return fmt.Errorf("%q is inside the git metadata directory and off-limits", s)
		}
		// Never let the agent rewrite coordination/lock files.
		base := filepath.Base(p)
		if base == StateFile || strings.HasPrefix(base, ".state.lock") || strings.HasPrefix(base, ".pipeline.lock") {
			return fmt.Errorf("%q is pipeline-internal and read-only for you", s)
		}
	}
	return nil
}
