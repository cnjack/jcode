package team

import (
	"fmt"
	"strings"
	"time"
)

const (
	AgentTypeExplore = "explore"
	AgentTypeGeneral = "general"
	AgentTypeCoder   = "coder"

	PermissionNormal = "normal"
	PermissionPlan   = "plan"
	PermissionAuto   = "auto"
)

// NormalizeAgentType returns the canonical teammate profile. An omitted value
// preserves the historical full-tool teammate behavior by defaulting to
// general; unknown profiles are rejected instead of accidentally receiving a
// broader tool set.
func NormalizeAgentType(agentType string) (string, error) {
	agentType = strings.TrimSpace(agentType)
	if agentType == "" {
		return AgentTypeGeneral, nil
	}
	switch agentType {
	case AgentTypeExplore, AgentTypeGeneral, AgentTypeCoder:
		return agentType, nil
	default:
		return "", fmt.Errorf("agent_type must be %q, %q, or %q, got %q",
			AgentTypeExplore, AgentTypeGeneral, AgentTypeCoder, agentType)
	}
}

// NormalizePermission returns the canonical teammate permission mode. Empty
// means normal, where each mutating child call still shares the leader's
// approval gate. Unknown modes fail closed.
func NormalizePermission(permission string) (string, error) {
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return PermissionNormal, nil
	}
	switch permission {
	case PermissionNormal, PermissionPlan, PermissionAuto:
		return permission, nil
	default:
		return "", fmt.Errorf("permission mode must be %q, %q, or %q, got %q",
			PermissionNormal, PermissionPlan, PermissionAuto, permission)
	}
}

// TeamFile is the persistent team state, stored at ~/.jcode/teams/{name}/team.json.
type TeamFile struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	LeadAgentID string       `json:"lead_agent_id"`
	Members     []TeamMember `json:"members"`
}

// TeamMember represents a single agent in the team.
type TeamMember struct {
	AgentID    string    `json:"agent_id"`             // "{name}@{team}"
	Name       string    `json:"name"`                 // display name
	AgentType  string    `json:"agent_type,omitempty"` // "explore", "general", "coder"
	Model      string    `json:"model,omitempty"`      // model override
	Prompt     string    `json:"prompt,omitempty"`     // initial task prompt
	Color      string    `json:"color,omitempty"`      // TUI color
	Cwd        string    `json:"cwd"`                  // working directory
	JoinedAt   time.Time `json:"joined_at"`            // when the agent joined
	IsActive   bool      `json:"is_active"`            // currently active?
	Permission string    `json:"permission,omitempty"` // "normal", "plan", "auto"
}

// TeammateStatus represents the lifecycle state of a running teammate.
type TeammateStatus string

const (
	StatusPending   TeammateStatus = "pending"
	StatusRunning   TeammateStatus = "running"
	StatusIdle      TeammateStatus = "idle"
	StatusCompleted TeammateStatus = "completed"
	StatusFailed    TeammateStatus = "failed"
	StatusKilled    TeammateStatus = "killed"
)

// IsTerminal returns true for states that represent a finished agent.
func (s TeammateStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusKilled
}

// TeammateIdentity holds the identity info for a running teammate.
type TeammateIdentity struct {
	AgentID   string
	AgentName string
	TeamName  string
	Color     string
}

// FormatAgentID returns the canonical agent ID: "name@team".
func FormatAgentID(name, teamName string) string {
	return name + "@" + teamName
}

// TeammateMessage is a single message stored in a teammate's mailbox file.
type TeammateMessage struct {
	From      string    `json:"from"`
	Text      string    `json:"text"`
	Summary   string    `json:"summary,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Read      bool      `json:"read"`
	Color     string    `json:"color,omitempty"`
}

// AgentProgress tracks a teammate's execution progress for UI display.
type AgentProgress struct {
	ToolCallCount int
	TokensUsed    int64
	LastToolName  string
	LastToolTime  time.Time
}

// SpawnConfig holds configuration for spawning a new teammate.
type SpawnConfig struct {
	Name       string
	Prompt     string
	AgentType  string // "explore", "coder", "general"
	Model      string // optional model override
	Cwd        string // optional working directory
	Permission string // "normal", "plan", "auto"
}

// TeamLeadName is the canonical name for the team leader.
const TeamLeadName = "team-lead"

// Agent colors for round-robin assignment.
var agentColors = []string{
	"#FF6B6B", // red
	"#4ECDC4", // teal
	"#FFE66D", // yellow
	"#95E1D3", // mint
	"#F38181", // coral
	"#AA96DA", // purple
	"#A8E6CF", // green
	"#DDA0DD", // plum
}
