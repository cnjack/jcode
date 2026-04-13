package team

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
)

const (
	maxTeammateMessages = 50
	mailboxPollInterval = 500 * time.Millisecond
	evictGracePeriod    = 30 * time.Second
	teammateMaxIter     = 200
)

// TeammateState holds the runtime state of a running teammate.
type TeammateState struct {
	Identity    TeammateIdentity
	TaskID      string
	Status      TeammateStatus
	Env         any // *tools.Env, kept as any to avoid import cycle
	Cancel      context.CancelFunc
	WorkCancel  context.CancelFunc
	History     []adk.Message     // full conversation history across turns (analogous to Claude Code's allMessages)
	Messages    []*schema.Message // capped at maxTeammateMessages for UI display only
	Pending     []string          // pending user messages from TUI input
	Progress    *AgentProgress
	Recorder    *session.Recorder // per-teammate session recorder (JSONL)
	IsIdle      bool
	ShutdownReq bool
	Error       string
	StartedAt   time.Time
	EvictAfter  *time.Time
	Prompt      string
	Model       string
	AgentType   string
	Permission  string
}

// ManagerDeps holds dependencies injected into the TeamManager.
// The EnvFactory and ToolBuilder use any for Env to avoid import cycles with tools package.
type ManagerDeps struct {
	TuiProgram *tea.Program
	// EnvFactory creates an isolated Env (any = *tools.Env) given a working directory.
	EnvFactory func(cwd string) any
	// ToolBuilder returns tools for a given Env and agent type.
	ToolBuilder func(env any, agentType string) []tool.BaseTool
	// ModelFactory returns a chat model for the given model name, or nil for default.
	ModelFactory func(ctx context.Context, modelName string) (any, error)
	// DefaultModel is the default chat model to use.
	DefaultModel  any // model.ToolCallingChatModel
	PromptBuilder func(agentType, pwd, platform string) string
	ApprovalFunc  func(ctx context.Context, toolName, toolArgs string) (bool, error)
	// HandlersFactory returns agent middleware handlers (including approval) for a teammate.
	// The workerName and workerColor are used to tag approval prompts.
	HandlersFactory func(workerName, workerColor string) []adk.ChatModelAgentMiddleware
	// LeaderSessionUUID is used to store teammate transcripts under the leader's session.
	LeaderSessionUUID string
}

// Manager coordinates a team of agents.
type Manager struct {
	mu         sync.RWMutex
	teamName   string
	teamFile   *TeamFile
	teammates  map[string]*TeammateState // agentID → state
	mailbox    *Mailbox
	deps       *ManagerDeps
	colorIndex int
	baseDir    string
}

// NewManager creates a team manager (without creating a team yet).
func NewManager(deps *ManagerDeps) *Manager {
	return &Manager{
		teammates: make(map[string]*TeammateState),
		deps:      deps,
	}
}

// SetTuiProgram sets the TUI program reference on the manager (called after RunTUI).
func (m *Manager) SetTuiProgram(p *tea.Program) {
	m.deps.TuiProgram = p
}

// SetHandlersFactory sets the factory that creates agent middleware handlers for teammates.
func (m *Manager) SetHandlersFactory(f func(workerName, workerColor string) []adk.ChatModelAgentMiddleware) {
	m.deps.HandlersFactory = f
}

// HasTeam returns true if a team is currently active.
func (m *Manager) HasTeam() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teamName != ""
}

// TeamName returns the current team name.
func (m *Manager) TeamName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teamName
}

// LeadAgentID returns the lead agent's ID.
func (m *Manager) LeadAgentID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.teamFile == nil {
		return ""
	}
	return m.teamFile.LeadAgentID
}

// CreateTeam initializes a new team with the caller as leader.
func (m *Manager) CreateTeam(name, description string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.teamName != "" {
		return fmt.Errorf("already leading team %q", m.teamName)
	}

	leadID := FormatAgentID(TeamLeadName, name)
	tf := &TeamFile{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		LeadAgentID: leadID,
		Members: []TeamMember{{
			AgentID:  leadID,
			Name:     TeamLeadName,
			JoinedAt: time.Now(),
			IsActive: true,
		}},
	}

	m.baseDir = filepath.Join(config.ConfigDir(), "teams", name)
	if err := os.MkdirAll(m.baseDir, 0755); err != nil {
		return fmt.Errorf("create team dir: %w", err)
	}

	if err := m.writeTeamFile(tf); err != nil {
		return err
	}

	m.teamName = name
	m.teamFile = tf
	m.mailbox = NewMailbox(name, m.deps.LeaderSessionUUID)
	m.colorIndex = 0

	config.Logger().Printf("[team] created team %q, lead=%s", name, leadID)
	return nil
}

// SpawnTeammate starts a new teammate goroutine.
func (m *Manager) SpawnTeammate(ctx context.Context, cfg SpawnConfig) (string, error) {
	m.mu.Lock()

	if m.teamName == "" {
		m.mu.Unlock()
		return "", fmt.Errorf("no active team")
	}

	agentID := FormatAgentID(cfg.Name, m.teamName)

	// Check for duplicate.
	if _, exists := m.teammates[agentID]; exists {
		m.mu.Unlock()
		return "", fmt.Errorf("teammate %q already exists", cfg.Name)
	}

	color := m.nextColor()

	// Create isolated environment via factory.
	cwd := cfg.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	var childEnv any
	if m.deps.EnvFactory != nil {
		childEnv = m.deps.EnvFactory(cwd)
	}

	childCtx, cancel := context.WithCancel(ctx)
	childCtx = WithTeammateContext(childCtx, TeammateIdentity{
		AgentID:   agentID,
		AgentName: cfg.Name,
		TeamName:  m.teamName,
		Color:     color,
	})

	state := &TeammateState{
		Identity: TeammateIdentity{
			AgentID:   agentID,
			AgentName: cfg.Name,
			TeamName:  m.teamName,
			Color:     color,
		},
		TaskID:     agentID,
		Status:     StatusPending,
		Env:        childEnv,
		Cancel:     cancel,
		Prompt:     cfg.Prompt,
		Model:      cfg.Model,
		AgentType:  cfg.AgentType,
		Permission: cfg.Permission,
		StartedAt:  time.Now(),
	}

	// Create per-teammate session recorder for conversation persistence.
	if m.deps.LeaderSessionUUID != "" {
		rec, _ := session.NewTeammateRecorder(m.deps.LeaderSessionUUID, agentID, cfg.Model)
		state.Recorder = rec
	}

	m.teammates[agentID] = state

	// Add member to team file.
	m.teamFile.Members = append(m.teamFile.Members, TeamMember{
		AgentID:    agentID,
		Name:       cfg.Name,
		AgentType:  cfg.AgentType,
		Model:      cfg.Model,
		Prompt:     cfg.Prompt,
		Color:      color,
		Cwd:        cwd,
		JoinedAt:   time.Now(),
		IsActive:   true,
		Permission: cfg.Permission,
	})
	_ = m.writeTeamFile(m.teamFile)

	m.mu.Unlock()

	// Start the teammate goroutine.
	go m.runTeammate(childCtx, state)

	// Notify TUI.
	if m.deps.TuiProgram != nil {
		m.deps.TuiProgram.Send(TeammateSpawnedMsg{
			AgentID: agentID,
			Name:    cfg.Name,
			Color:   color,
			Prompt:  cfg.Prompt,
		})
	}

	config.Logger().Printf("[team] spawned teammate %q (type=%s, model=%s)", cfg.Name, cfg.AgentType, cfg.Model)
	return agentID, nil
}

// SendMessage sends a message to a specific teammate's mailbox.
func (m *Manager) SendMessage(to, from, message, summary string) error {
	m.mu.RLock()
	mb := m.mailbox
	m.mu.RUnlock()

	if mb == nil {
		return fmt.Errorf("no active team")
	}

	fromColor := m.getTeammateColor(from)
	return mb.WriteMessage(to, TeammateMessage{
		From:      from,
		Text:      message,
		Summary:   summary,
		Timestamp: time.Now(),
		Color:     fromColor,
	})
}

// BroadcastMessage sends a message to all teammates except the sender.
func (m *Manager) BroadcastMessage(from, message, summary string) (int, error) {
	m.mu.RLock()
	mb := m.mailbox
	teammates := make(map[string]*TeammateState, len(m.teammates))
	for k, v := range m.teammates {
		teammates[k] = v
	}
	m.mu.RUnlock()

	if mb == nil {
		return 0, fmt.Errorf("no active team")
	}

	count := 0
	fromColor := m.getTeammateColor(from)
	for _, t := range teammates {
		if t.Identity.AgentName == from {
			continue
		}
		if err := mb.WriteMessage(t.Identity.AgentName, TeammateMessage{
			From:      from,
			Text:      message,
			Summary:   summary,
			Timestamp: time.Now(),
			Color:     fromColor,
		}); err != nil {
			config.Logger().Printf("[team] broadcast err for %s: %v", t.Identity.AgentName, err)
			continue
		}
		count++
	}
	// Also send to leader if sender is not leader.
	if from != TeamLeadName {
		_ = mb.WriteMessage(TeamLeadName, TeammateMessage{
			From:      from,
			Text:      message,
			Summary:   summary,
			Timestamp: time.Now(),
			Color:     fromColor,
		})
		count++
	}
	return count, nil
}

// ShutdownTeammate sends a shutdown request to a teammate.
func (m *Manager) ShutdownTeammate(name, reason string) error {
	shutdownMsg, _ := json.Marshal(map[string]string{
		"type":   "shutdown_request",
		"reason": reason,
	})
	return m.SendMessage(name, TeamLeadName, string(shutdownMsg), "shutdown request")
}

// KillTeammate forcefully terminates a teammate.
func (m *Manager) KillTeammate(name string) error {
	m.mu.Lock()
	agentID := FormatAgentID(name, m.teamName)
	state, ok := m.teammates[agentID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("teammate %q not found", name)
	}
	state.Cancel()
	state.Status = StatusKilled
	m.mu.Unlock()

	if m.deps.TuiProgram != nil {
		m.deps.TuiProgram.Send(TeammateStatusMsg{
			AgentID: agentID,
			Status:  StatusKilled,
		})
	}
	return nil
}

// DissolveTeam shuts down all teammates and cleans up the team.
func (m *Manager) DissolveTeam(ctx context.Context) error {
	m.mu.Lock()
	if m.teamName == "" {
		m.mu.Unlock()
		return fmt.Errorf("no active team")
	}
	teamName := m.teamName

	// Cancel all teammates.
	for _, t := range m.teammates {
		t.Cancel()
		t.Status = StatusKilled
	}

	m.teamName = ""
	m.teamFile = nil
	m.teammates = make(map[string]*TeammateState)
	m.mailbox = nil
	m.mu.Unlock()

	config.Logger().Printf("[team] dissolved team %q", teamName)
	return nil
}

// GetTeammateState returns a snapshot of a teammate's state.
func (m *Manager) GetTeammateState(agentID string) *TeammateState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teammates[agentID]
}

// ListTeammates returns all non-leader teammates sorted alphabetically by agent name.
func (m *Manager) ListTeammates() []*TeammateState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*TeammateState, 0, len(m.teammates))
	for _, t := range m.teammates {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Identity.AgentName < result[j].Identity.AgentName
	})
	return result
}

// EnqueueUserMessage adds a user message to a teammate's pending queue
// (for input from TUI when viewing that teammate).
func (m *Manager) EnqueueUserMessage(agentID, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.teammates[agentID]; ok {
		t.Pending = append(t.Pending, message)
	}
}

// runTeammate is the main loop for each teammate goroutine.
// Like Claude Code's runInProcessTeammate, it maintains a persistent allMessages
// (state.History) that accumulates the full conversation across multiple turns.
//
// Unlike previous behavior, the teammate does NOT auto-start a conversation.
// It enters idle state immediately and waits for an explicit message from the
// leader or another teammate before running its first agent turn.
// The spawn prompt is used as system context, not as user input.
func (m *Manager) runTeammate(ctx context.Context, state *TeammateState) {
	defer m.cleanupTeammate(state)

	// Initialize empty history; prompt is system context, not user input.
	state.History = nil

	// Start in idle state, waiting for the first message.
	m.updateStatus(state, StatusIdle)
	state.IsIdle = true

	for {
		msg, err := m.waitForMessage(ctx, state)
		if err != nil {
			if ctx.Err() != nil {
				m.updateStatus(state, StatusKilled)
				return
			}
			continue
		}

		state.IsIdle = false
		currentPrompt := msg.Text

		// Notify TUI of incoming user message.
		if m.deps.TuiProgram != nil {
			m.deps.TuiProgram.Send(TeammateMessageMsg{
				AgentID: state.Identity.AgentID,
				Role:    "user",
				Content: msg.Text,
				From:    msg.From,
			})
		}

		// Handle shutdown requests specially.
		if IsShutdownRequest(msg.Text) {
			state.ShutdownReq = true
			currentPrompt = fmt.Sprintf("[System] The team lead has requested your shutdown. Reason: %s\nPlease finish your current work and respond with a summary.", msg.Text)
		}

		// Append the new user message to persistent history.
		state.History = append(state.History, schema.UserMessage(currentPrompt))
		if state.Recorder != nil {
			state.Recorder.RecordUser(currentPrompt)
		}

		m.updateStatus(state, StatusRunning)

		// Run one agent turn.
		result, err := m.runAgentTurn(ctx, state)
		if err != nil {
			if ctx.Err() != nil {
				m.updateStatus(state, StatusKilled)
				return
			}
			config.Logger().Printf("[team] %s agent error: %v", state.Identity.AgentName, err)
			state.Error = err.Error()
			m.updateStatus(state, StatusFailed)
			return
		}

		// Append assistant response to persistent history.
		if result != "" {
			state.History = append(state.History, &schema.Message{
				Role: schema.Assistant, Content: result,
			})
			if state.Recorder != nil {
				state.Recorder.RecordAssistant(result)
			}
		}

		// Check if shutdown was approved.
		if state.ShutdownReq {
			m.updateStatus(state, StatusCompleted)
			return
		}

		// Mark idle, wait for next message.
		m.updateStatus(state, StatusIdle)
		state.IsIdle = true
	}
}

// runAgentTurn creates an agent and runs it for one conversation turn.
// It uses the full accumulated state.History so teammates maintain context across turns.
func (m *Manager) runAgentTurn(ctx context.Context, state *TeammateState) (string, error) {
	// Resolve model.
	var chatModel any = m.deps.DefaultModel
	if state.Model != "" && m.deps.ModelFactory != nil {
		resolved, err := m.deps.ModelFactory(ctx, state.Model)
		if err != nil {
			config.Logger().Printf("[team] %s model resolve err: %v", state.Identity.AgentName, err)
		} else {
			chatModel = resolved
		}
	}

	// Build tools.
	agentType := state.AgentType
	if agentType == "" {
		agentType = "general"
	}
	var childTools []tool.BaseTool
	if m.deps.ToolBuilder != nil {
		childTools = m.deps.ToolBuilder(state.Env, agentType)
	}

	cwd := ""
	if cfg, ok := state.Env.(interface{ Pwd() string }); ok {
		cwd = cfg.Pwd()
	}

	// Build system prompt. Include spawn prompt as context/role description.
	systemPrompt := ""
	if m.deps.PromptBuilder != nil {
		systemPrompt = m.deps.PromptBuilder(agentType, cwd, cwd)
	}
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf("You are %q, a teammate in team %q. Your role: %s.",
			state.Identity.AgentName, state.Identity.TeamName, agentType)
	}
	if state.Prompt != "" {
		systemPrompt += "\n\n## Your Task\n" + state.Prompt
	}

	// Create agent with approval middleware.
	chatModelTyped, ok := chatModel.(einomodel.ToolCallingChatModel)
	if !ok {
		return "", fmt.Errorf("invalid chat model type")
	}

	var handlers []adk.ChatModelAgentMiddleware
	if m.deps.HandlersFactory != nil {
		handlers = m.deps.HandlersFactory(state.Identity.AgentName, state.Identity.Color)
	}

	ag, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        fmt.Sprintf("teammate-%s", state.Identity.AgentName),
		Description: fmt.Sprintf("Teammate %s in team %s", state.Identity.AgentName, state.Identity.TeamName),
		Instruction: systemPrompt,
		Model:       chatModelTyped,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: childTools,
			},
		},
		MaxIterations: teammateMaxIter,
		Handlers:      handlers,
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries:  3,
			IsRetryAble: internalmodel.IsRetryable,
			BackoffFunc: internalmodel.SmartBackoff,
		},
	})
	if err != nil {
		return "", fmt.Errorf("create agent: %w", err)
	}

	// Use full persistent history for multi-turn conversation continuity.
	// This mirrors Claude Code's allMessages pattern where the complete
	// conversation is passed to each agent invocation.
	input := &adk.AgentInput{
		Messages:        state.History,
		EnableStreaming: true,
	}

	var result strings.Builder
	iterator := ag.Run(ctx, input)
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return result.String(), event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mo := event.Output.MessageOutput

		if mo.Role == schema.Assistant {
			if !mo.IsStreaming && mo.Message != nil {
				// Send tool call events first.
				if m.deps.TuiProgram != nil {
					for _, tc := range mo.Message.ToolCalls {
						if tc.Function.Name != "" {
							m.deps.TuiProgram.Send(TeammateMessageMsg{
								AgentID:  state.Identity.AgentID,
								Role:     "tool_call",
								ToolName: tc.Function.Name,
								ToolArgs: tc.Function.Arguments,
							})
						}
					}
				}
				result.WriteString(mo.Message.Content)
				m.appendMessage(state, mo.Message)
				if m.deps.TuiProgram != nil && mo.Message.Content != "" {
					m.deps.TuiProgram.Send(TeammateMessageMsg{
						AgentID: state.Identity.AgentID,
						Role:    "assistant",
						Content: mo.Message.Content,
					})
				}
			} else if mo.IsStreaming {
				var sb strings.Builder
				for {
					chunk, err := mo.MessageStream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						break
					}
					if chunk != nil {
						// Send tool call events from streaming chunks.
						if m.deps.TuiProgram != nil {
							for _, tc := range chunk.ToolCalls {
								if tc.Function.Name != "" {
									m.deps.TuiProgram.Send(TeammateMessageMsg{
										AgentID:  state.Identity.AgentID,
										Role:     "tool_call",
										ToolName: tc.Function.Name,
										ToolArgs: tc.Function.Arguments,
									})
								}
							}
						}
						sb.WriteString(chunk.Content)
					}
				}
				if sb.Len() > 0 {
					result.WriteString(sb.String())
					m.appendMessage(state, &schema.Message{Role: schema.Assistant, Content: sb.String()})
					if m.deps.TuiProgram != nil {
						m.deps.TuiProgram.Send(TeammateMessageMsg{
							AgentID: state.Identity.AgentID,
							Role:    "assistant",
							Content: sb.String(),
						})
					}
				}
			}
		} else if mo.Role == schema.Tool {
			if state.Progress == nil {
				state.Progress = &AgentProgress{}
			}
			state.Progress.ToolCallCount++
			state.Progress.LastToolName = mo.ToolName
			state.Progress.LastToolTime = time.Now()
			if m.deps.TuiProgram != nil {
				m.deps.TuiProgram.Send(TeammateProgressMsg{
					AgentID:   state.Identity.AgentID,
					ToolName:  mo.ToolName,
					ToolCount: state.Progress.ToolCallCount,
				})
				// Send tool result event for TUI display.
				var toolContent string
				var toolErr string
				if !mo.IsStreaming && mo.Message != nil {
					toolContent = mo.Message.Content
				} else if mo.IsStreaming {
					var sb strings.Builder
					for {
						chunk, err := mo.MessageStream.Recv()
						if err == io.EOF {
							break
						}
						if err != nil {
							toolErr = err.Error()
							break
						}
						if chunk != nil {
							sb.WriteString(chunk.Content)
						}
					}
					toolContent = sb.String()
				}
				m.deps.TuiProgram.Send(TeammateMessageMsg{
					AgentID:  state.Identity.AgentID,
					Role:     "tool_result",
					Content:  toolContent,
					ToolName: mo.ToolName,
					ToolErr:  toolErr,
				})
			}
		}
	}

	return result.String(), nil
}

// waitForMessage polls the mailbox and pending queue for new messages.
func (m *Manager) waitForMessage(ctx context.Context, state *TeammateState) (*TeammateMessage, error) {
	ticker := time.NewTicker(mailboxPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// 1. Check pending user messages (from TUI direct input).
			m.mu.Lock()
			if len(state.Pending) > 0 {
				msg := state.Pending[0]
				state.Pending = state.Pending[1:]
				m.mu.Unlock()
				return &TeammateMessage{Text: msg, From: "user", Timestamp: time.Now()}, nil
			}
			m.mu.Unlock()

			// 2. Check mailbox for unread messages.
			if m.mailbox == nil {
				continue
			}
			msg, err := m.mailbox.ConsumeFirst(state.Identity.AgentName)
			if err != nil || msg == nil {
				continue
			}
			return msg, nil
		}
	}
}

// appendMessage appends a message to the teammate's UI message buffer (capped).
func (m *Manager) appendMessage(state *TeammateState, msg *schema.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state.Messages = append(state.Messages, msg)
	if len(state.Messages) > maxTeammateMessages {
		state.Messages = state.Messages[len(state.Messages)-maxTeammateMessages:]
	}
}

// updateStatus changes a teammate's status and notifies TUI.
func (m *Manager) updateStatus(state *TeammateState, status TeammateStatus) {
	m.mu.Lock()
	state.Status = status
	if status.IsTerminal() {
		t := time.Now().Add(evictGracePeriod)
		state.EvictAfter = &t
	}
	m.mu.Unlock()

	if m.deps.TuiProgram != nil {
		m.deps.TuiProgram.Send(TeammateStatusMsg{
			AgentID: state.Identity.AgentID,
			Status:  status,
			Error:   state.Error,
		})
	}
}

// cleanupTeammate performs final cleanup when a teammate goroutine exits.
func (m *Manager) cleanupTeammate(state *TeammateState) {
	// Close the per-teammate session recorder.
	if state.Recorder != nil {
		state.Recorder.Close()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Update team file.
	if m.teamFile != nil {
		for i, mb := range m.teamFile.Members {
			if mb.AgentID == state.Identity.AgentID {
				m.teamFile.Members[i].IsActive = false
				break
			}
		}
		_ = m.writeTeamFile(m.teamFile)
	}

	config.Logger().Printf("[team] teammate %s cleaned up (status=%s)", state.Identity.AgentName, state.Status)
}

// getLeaderCwd tries to get the leader's working directory.
func (m *Manager) getLeaderCwd() string {
	// The leader doesn't have a TeammateState, so return empty.
	// The caller should provide a Cwd.
	cwd, _ := os.Getwd()
	return cwd
}

func (m *Manager) nextColor() string {
	c := agentColors[m.colorIndex%len(agentColors)]
	m.colorIndex++
	return c
}

func (m *Manager) getTeammateColor(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	agentID := FormatAgentID(name, m.teamName)
	if t, ok := m.teammates[agentID]; ok {
		return t.Identity.Color
	}
	return ""
}

func (m *Manager) writeTeamFile(tf *TeamFile) error {
	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal team file: %w", err)
	}
	path := filepath.Join(m.baseDir, "team.json")
	return os.WriteFile(path, data, 0644)
}
