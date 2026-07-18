package tools

import "strings"

var readOnlyShellPrograms = map[string]bool{
	"ls":    true,
	"pwd":   true,
	"cat":   true,
	"echo":  true,
	"which": true,
}

var readOnlyGitSubcommands = map[string]bool{
	"status": true,
	"log":    true,
	"diff":   true,
	"show":   true,
}

// IsReadOnlyShellCommand reports whether command belongs to the deliberately
// small shell allowlist shared by Plan execution and approval auto-approval.
// It is intentionally conservative: shell syntax, expansions, quoting, globs,
// and git options that can write files or launch helpers are all rejected.
func IsReadOnlyShellCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || containsShellSyntax(command) {
		return false
	}

	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "env":
		// Bare env only prints the environment. Any argument can name a program
		// to execute, so it leaves the read-only allowlist.
		return len(fields) == 1
	case "git":
		return isReadOnlyGitCommand(fields)
	default:
		return readOnlyShellPrograms[fields[0]]
	}
}

func containsShellSyntax(command string) bool {
	// Quotes and escapes are rejected too: without a full shell parser they can
	// hide an option from this policy while the shell still reconstructs it.
	const shellSyntax = ";&|<>`\n\r()$\\\"'*!?[]{}"
	return strings.ContainsAny(command, shellSyntax)
}

func isReadOnlyGitCommand(fields []string) bool {
	if len(fields) < 2 || !readOnlyGitSubcommands[fields[1]] {
		return false
	}
	for _, arg := range fields[2:] {
		if isDangerousGitReadOption(arg) {
			return false
		}
	}
	return true
}

func isDangerousGitReadOption(arg string) bool {
	// These explicit negations disable helper execution and are safe.
	if arg == "--no-ext-diff" || arg == "--no-textconv" || arg == "--no-pager" {
		return false
	}
	if arg == "-c" {
		return true
	}
	// Git accepts abbreviated long options, so reject prefixes rather than only
	// their full spellings. --output writes a file; --ext-diff/--textconv and
	// exec/config/pager options can launch caller- or config-selected programs.
	dangerousPrefixes := []string{
		"--out",
		"--ext",
		"--text",
		"--exec",
		"--config",
		"--paginate",
	}
	for _, prefix := range dangerousPrefixes {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}
