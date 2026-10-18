package command

import (
	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
)

// sessionViewTools builds the read-only cross-session view tools for one
// transport (design §5.9).
//
// It returns nil when the capability is disabled, so the off-switch removes all
// three tools from every transport: TUI (interactive.go buildAllTools /
// buildPlanTools), ACP (acp.go allTools / planTools), and Web (web.go all /
// plan). A nil config fails closed (config.SessionViewEnabled).
//
// The resolved budgets are published onto the Env before the tools are built so
// all three share one snapshot for this agent build (§5.8).
func sessionViewTools(env *tools.Env, cfg *config.Config) []tool.BaseTool {
	if env == nil || !config.SessionViewEnabled(cfg) {
		return nil
	}
	env.SetSessionViewSettings(config.SessionViewSettings(cfg))
	return []tool.BaseTool{
		env.NewSessionListTool(),
		env.NewSessionSearchTool(),
		env.NewSessionReadTool(),
	}
}

// wireSessionCaller publishes the recorder-backed caller identity used by the
// cross-session view tools.
//
// SessionIDFn and SessionCallerFn are deliberately derived from the SAME
// recorder in the same call (N11): the recorder's UUID and project are the only
// identity source, and a tool argument can never influence them. The closures
// capture the *Recorder pointer and read it live, so a SetUUID on that same
// recorder (the resume path) is followed rather than cached. Known residual
// (R6): internal/web/chat.go replaces the recorder OBJECT on one defensive
// rebind path, which a closure built from the previous recorder does not see —
// it keeps reporting that object's UUID. It cannot widen access (the
// replacement is scoped to the same project), so this is accepted, not
// followed. Callers must nil-check rec before calling (N6): this helper
// tolerates nil, but the transports keep their explicit `if rec != nil` guard
// so no recorder method is ever invoked on nil.
func wireSessionCaller(env *tools.Env, rec *session.Recorder) {
	if env == nil || rec == nil {
		return
	}
	env.SessionIDFn = rec.UUID
	env.SessionCallerFn = func() tools.SessionCaller {
		return tools.SessionCaller{SessionID: rec.UUID(), Project: rec.Project()}
	}
}
