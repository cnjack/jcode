package tools

import (
	"path/filepath"
	"strings"
)

// CommandCategory classifies a shell command for UI and policy decisions.
type CommandCategory string

const (
	CmdSearch   CommandCategory = "search"
	CmdRead     CommandCategory = "read"
	CmdList     CommandCategory = "list"
	CmdSafe     CommandCategory = "safe"
	CmdGit      CommandCategory = "git"
	CmdMutating CommandCategory = "mutating"
)

var categoryMap = map[string]CommandCategory{
	// search
	"grep": CmdSearch, "rg": CmdSearch, "find": CmdSearch, "ag": CmdSearch,
	// read
	"cat": CmdRead, "head": CmdRead, "tail": CmdRead, "less": CmdRead,
	"jq": CmdRead, "wc": CmdRead,
	// list
	"ls": CmdList, "tree": CmdList, "du": CmdList, "df": CmdList,
	// safe
	"pwd": CmdSafe, "echo": CmdSafe, "date": CmdSafe, "whoami": CmdSafe,
	"uname": CmdSafe, "which": CmdSafe, "env": CmdSafe, "printenv": CmdSafe,
}

var gitReadSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
	"branch": true, "tag": true, "remote": true, "config": true,
}

// classifyCommand determines the category of a shell command based on the
// first token (base name) and, for git, the subcommand.
func classifyCommand(cmd string) CommandCategory {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return CmdMutating
	}

	tokens := strings.Fields(cmd)
	base := filepath.Base(tokens[0])

	if base == "git" {
		if len(tokens) > 1 && gitReadSubcommands[tokens[1]] {
			return CmdGit
		}
		return CmdMutating
	}

	if cat, ok := categoryMap[base]; ok {
		return cat
	}
	return CmdMutating
}

// IsCollapsible returns true for categories whose output is read-only and
// can be collapsed in the TUI to save space.
func (c CommandCategory) IsCollapsible() bool {
	switch c {
	case CmdSearch, CmdRead, CmdList, CmdGit:
		return true
	default:
		return false
	}
}
