package tui

import (
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/mode"
)

const maxToolOutputLen = 500

var promptCh = make(chan string, 1)

var pendingPromptCh = make(chan string, 16)

var sshCh = make(chan interface{}, 1)

func GetPromptChannel() <-chan string {
	return promptCh
}

func GetPendingPromptChannel() <-chan string {
	return pendingPromptCh
}

func GetSSHChannel() <-chan interface{} {
	return sshCh
}

var configCh = make(chan *config.Config, 1)

// GetConfigChannel returns the channel that receives configuration changes.
func GetConfigChannel() <-chan *config.Config {
	return configCh
}

// addModelCh is used to notify main goroutine to launch add-model setup wizard.
var addModelCh = make(chan struct{}, 1)

// GetAddModelChannel returns the channel that receives add-model requests.
func GetAddModelChannel() <-chan struct{} {
	return addModelCh
}

// resumeCh is used to pass a selected session UUID from TUI to the main goroutine.
var resumeCh = make(chan string, 1)

// GetResumeChannel returns the channel that receives session resume requests.
func GetResumeChannel() <-chan string {
	return resumeCh
}

// approvalCh is used to pass tool approval requests from main goroutine to TUI.
var approvalCh = make(chan ToolApprovalRequestMsg, 1)

// GetApprovalChannel returns the channel that receives tool approval requests.
func GetApprovalChannel() chan ToolApprovalRequestMsg {
	return approvalCh
}

// --- Messages ---

type AgentTextMsg struct{ Text string }
type ToolCallMsg struct {
	Name     string
	Args     string
	Title    string // human-readable tool name (e.g. "Read", "Shell")
	Subtitle string // context info (file path, description)
}
type ToolResultMsg struct {
	Name, Output string
	Err          error
}
type AgentDoneMsg struct{ Err error }
type PromptSubmitMsg struct{ Prompt string }
type UserPromptMsg struct{ Prompt string }

// BatchRenderMsg is sent by the stream debounce timer to batch-flush
// accumulated AgentTextMsg content into a single viewport refresh.
type BatchRenderMsg struct{}

// TodoUpdateMsg signals that the todo store has been updated.
type TodoUpdateMsg struct{}

// AddModelMsg signals that the user wants to add a new model via setup wizard
type AddModelMsg struct{}

// ResumeRequestMsg is sent when the user requests to resume a session by UUID.
type ResumeRequestMsg struct{ UUID string }

// SessionEntry is a display-ready record from a replayed session.
type SessionEntry struct {
	Type         string
	Content      string
	Name         string
	Args         string
	Output       string
	Error        string
	ToolCallID   string
	SubagentName string
	SubagentType string

	// Plan fields
	PlanStatus  string
	PlanTitle   string
	PlanContent string
	Feedback    string

	// Todo fields
	Todos []TodoSnapshotItem

	// Mode change
	Mode string

	// Compact fields
	Summary    string
	CompactedN int
}

// TodoSnapshotItem mirrors session.TodoSnapshotItem for TUI display.
type TodoSnapshotItem struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// SessionResumedMsg is sent by the main goroutine to replay a session in the TUI.
type SessionResumedMsg struct {
	UUID    string
	Entries []SessionEntry
}

// AgentsMdMsg is sent by the main goroutine to notify TUI that agents.md was loaded.
type AgentsMdMsg struct {
	Found bool
	Path  string
}

// TokenUpdateMsg is sent periodically to update token usage display
type TokenUpdateMsg struct {
	TotalTokens       int64
	ModelContextLimit int // 0 if unknown
}

// ApprovalMode represents the approval mode state
type ApprovalMode int

const (
	ModeManual ApprovalMode = iota // Manual approval mode (default)
	ModeAuto                       // Auto-approve mode
)

// ToolApprovalRequestMsg is sent when a tool needs user approval
type ToolApprovalRequestMsg struct {
	Name        string
	Args        string
	Resp        chan ToolApprovalResponse
	IsExternal  bool   // Whether this is an external path access (for read tool)
	WorkerName  string // Non-empty when approval is from a teammate (e.g. "@backend")
	WorkerColor string // Teammate's color for TUI display
}

// ToolApprovalResponse is the user's response to a tool approval request
type ToolApprovalResponse struct {
	Approved bool
	Mode     ApprovalMode // Mode after this response (stay MANUAL or switch to AUTO)
}

// SSHConnectMsg is sent when user initially requests connection
type SSHConnectMsg struct {
	Addr string // user@host
	Path string // remote working dir (optional)
}

// SSHListDirReqMsg is sent when TUI needs to list a directory on the remote machine
type SSHListDirReqMsg struct {
	Path string
}

// SSHDirResultsMsg is sent from main to TUI with directory contents
type SSHDirResultsMsg struct {
	Path  string
	Items []string
	Err   error
}

// SSHStatusMsg carries the result of an SSH connection attempt.
type SSHStatusMsg struct {
	Success bool
	Label   string // e.g. "root@myserver:22"
	Err     error
}

// SSHCancelMsg is sent when user cancels the SSH dir picker via Esc.
type SSHCancelMsg struct{}

// ConfigUpdatedMsg is sent when the provider/model configuration is updated via setup wizard
type ConfigUpdatedMsg struct {
	Provider string
	Model    string
	Message  string
}

type MCPStatusItem struct {
	Name      string
	ToolCount int
	Running   bool
	ErrMsg    string
	// NeedsAuth is true when the server requires OAuth login before its tools
	// become available (use /mcp to log in).
	NeedsAuth bool
}

type MCPStatusMsg struct {
	Statuses []MCPStatusItem
}

// BgTaskDoneMsg is sent when a background task completes.
type BgTaskDoneMsg struct {
	TaskID  string
	Command string
	Status  string
	Output  string
}

// SubagentStartMsg is sent when a subagent begins executing.
type SubagentStartMsg struct {
	Name string
	Type string
}

// SubagentDoneMsg is sent when a subagent finishes.
type SubagentDoneMsg struct {
	Name   string
	Result string
	Err    error
}

// SubagentProgressMsg is sent for intermediate subagent events (tool calls / results).
type SubagentProgressMsg struct {
	AgentName string
	Event     string // "tool_call" or "tool_result"
	ToolName  string
	Detail    string
}

// SubagentTokenUpdateMsg is sent after each model turn to update the subagent's token usage.
type SubagentTokenUpdateMsg struct {
	TotalTokens int64 // cumulative tokens used by the subagent since it started
}

// CompactRequestMsg is sent when the user requests manual context compaction.
type CompactRequestMsg struct{}

// CompactDoneMsg is sent when context compaction completes.
type CompactDoneMsg struct {
	OldTokens int64
	NewTokens int64
	Err       error
}

// compactCh is used to notify main goroutine to compact context.
var compactCh = make(chan struct{}, 1)

// GetCompactChannel returns the channel that receives compact requests.
func GetCompactChannel() <-chan struct{} {
	return compactCh
}

// AgentMode represents the agent's operational mode.
type AgentMode int

const (
	ModeNormal    AgentMode = iota // Default mode
	ModePlanning                   // Plan mode — read-only exploration
	ModeExecuting                  // Executing an approved plan
)

// PlanApprovalMsg is sent when the agent wants to show a plan for approval.
type PlanApprovalMsg struct {
	PlanContent string
	PlanPath    string
}

// PlanApprovedMsg is sent when the user approves a plan.
type PlanApprovedMsg struct{}

// PlanRejectedMsg is sent when the user rejects a plan.
type PlanRejectedMsg struct {
	Feedback string
}

// planResponseCh carries plan approval/rejection from TUI to main goroutine.
var planResponseCh = make(chan PlanResponse, 1)

// PlanResponse is the user's response to a plan approval.
type PlanResponse struct {
	Approved bool
	Feedback string // non-empty on rejection
}

// GetPlanResponseChannel returns the channel for plan responses.
func GetPlanResponseChannel() <-chan PlanResponse {
	return planResponseCh
}

// modeSelectCh carries unified session-mode changes (Approval/Plan/Full access) from
// the TUI to the main goroutine. It replaces the old agent-mode-only channel so
// the single selector drives both the tool/prompt axis and the approval axis.
var modeSelectCh = make(chan mode.SessionMode, 1)

// GetModeSelectChannel returns the channel that receives session-mode changes.
func GetModeSelectChannel() <-chan mode.SessionMode {
	return modeSelectCh
}

// ModeSelectedMsg is sent from the main goroutine back to the TUI to sync the
// mode pill after the backend changes the session mode programmatically (resume
// restore, plan-completion revert, or echoing a Shift+Tab change).
type ModeSelectedMsg struct {
	Mode mode.SessionMode
}

// AskUserQuestionMsg is sent when the agent asks the user a question via ask_user tool.
type AskUserQuestionMsg struct {
	Question string
	Options  []string // optional selectable choices
}

// askUserResponseCh carries the user's answer from TUI back to the ask_user tool.
var askUserResponseCh = make(chan AskUserResponse, 1)

// AskUserResponse is the user's answer to an ask_user question.
type AskUserResponse struct {
	Answer string
}

// cancelAgentCh is used to signal the main goroutine to cancel a running agent job.
var cancelAgentCh = make(chan struct{}, 1)

// GetCancelAgentChannel returns the channel that receives agent cancellation requests.
func GetCancelAgentChannel() <-chan struct{} {
	return cancelAgentCh
}

// ExitTimeoutMsg is sent after 5s to clear exit confirmation
type ExitTimeoutMsg struct{}

// GetAskUserResponseChannel returns the channel for ask_user responses.
func GetAskUserResponseChannel() <-chan AskUserResponse {
	return askUserResponseCh
}

// SkillSlashMsg is sent when a user triggers a slash command mapped to a skill.
type SkillSlashMsg struct {
	SkillName string // skill name to load
	UserInput string // additional user input after the slash command
}

// SkillsLoadedMsg is sent at startup to inform the TUI about available skill slash commands.
type SkillsLoadedMsg struct {
	SlashCommands []SkillSlashInfo
}

// SkillSlashInfo describes a skill's slash command for TUI hint display.
type SkillSlashInfo struct {
	Slash       string
	Description string
}

// --- Channel (WeChat etc.) messages ---

// ChannelAction represents an action the user wants to perform on a channel.
type ChannelAction struct {
	ChannelID string // "wechat"
	Action    string // "login", "logout", "enable", "disable"
}

// channelActionCh carries channel actions from TUI to the main goroutine.
var channelActionCh = make(chan ChannelAction, 1)

// GetChannelActionChannel returns the channel for channel action events.
func GetChannelActionChannel() <-chan ChannelAction {
	return channelActionCh
}

// mcpLoginCh carries an MCP server name from the TUI to the main goroutine to
// start an OAuth login flow.
var mcpLoginCh = make(chan string, 1)

// GetMCPLoginChannel returns the channel for MCP OAuth login requests.
func GetMCPLoginChannel() <-chan string {
	return mcpLoginCh
}

// RequestMCPLogin asks the main goroutine to begin OAuth login for a server.
func RequestMCPLogin(name string) {
	select {
	case mcpLoginCh <- name:
	default:
	}
}

// MCPNoticeMsg is sent from the main goroutine to surface an MCP status line
// (login progress, errors) in the TUI transcript.
type MCPNoticeMsg struct {
	Text string
}

// ChannelStateMsg is sent from the main goroutine to update channel state display in TUI.
type ChannelStateMsg struct {
	ChannelID string // "wechat"
	State     string // "none", "disabled", "enabled"
	Message   string // status message (e.g. "connected", "scanning...")
}

// ChannelQRCodeMsg is sent when a QR code is available for scanning.
type ChannelQRCodeMsg struct {
	ChannelID     string
	QRCodeURL     string // image data or URL
	QRCodeContent string // raw content to encode as terminal QR
	Message       string
}

// ChannelInboundMsg is sent when an inbound message arrives from a channel.
type ChannelInboundMsg struct {
	ChannelID string
	From      string
	Text      string
}

// BLECommandMsg is sent when a command is received from a BLE device.
type BLECommandMsg struct {
	Cmd string // "input", "submit", "cancel"
	Val string // payload for "input" command
}
