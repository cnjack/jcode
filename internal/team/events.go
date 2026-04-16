package team

// TeammateSpawnedMsg notifies the TUI that a new teammate has started.
type TeammateSpawnedMsg struct {
	AgentID string
	Name    string
	Color   string
	Prompt  string
}

// TeammateStatusMsg notifies the TUI of a teammate status change.
type TeammateStatusMsg struct {
	AgentID string
	Status  TeammateStatus
	Error   string
}

// TeammateProgressMsg notifies the TUI of teammate progress.
type TeammateProgressMsg struct {
	AgentID   string
	ToolName  string
	ToolCount int
}

// TeammateMessageMsg notifies the TUI of a new message from a teammate.
type TeammateMessageMsg struct {
	AgentID  string
	Role     string // "assistant", "tool_call", "tool_result", "user"
	Content  string
	From     string // set when Role == "user" — who sent the message
	ToolName string // set when Role == "tool_call" or "tool_result"
	ToolArgs string // set when Role == "tool_call"
	ToolErr  string // set when Role == "tool_result" and there was an error
}

// TeammateTokenUpdateMsg notifies the TUI of a teammate's token usage update.
type TeammateTokenUpdateMsg struct {
	AgentID     string
	TotalTokens int64
}

// SetTeamManagerMsg is sent to the TUI to wire the team manager after program start.
type SetTeamManagerMsg struct {
	Manager *Manager
}
