package session

// WorkspaceKind describes how a conversation's execution directory relates to
// the user's project list. The zero value is deliberately treated as project
// so legacy session indexes remain project-bound after upgrade.
type WorkspaceKind string

const (
	WorkspaceProject WorkspaceKind = "project"
	WorkspaceScratch WorkspaceKind = "scratch"
)

// NormalizeWorkspaceKind maps legacy/unknown values to the safe project
// default. Only JCode-created workspaces may be marked scratch.
func NormalizeWorkspaceKind(kind WorkspaceKind) WorkspaceKind {
	if kind == WorkspaceScratch {
		return WorkspaceScratch
	}
	return WorkspaceProject
}
