package flow

import (
	"fmt"
	"strings"
)

// GetBySlash returns the workflow whose auto-generated "/<name>" trigger matches
// slash. Workflows only expose auto-generated slashes (see SlashCommands), so this
// is a name lookup with the leading "/" stripped.
func (l *Loader) GetBySlash(slash string) (Workflow, bool) {
	return l.Get(strings.TrimPrefix(slash, "/"))
}

// SlashRunPrompt is the agent instruction a "/<workflow>" slash command expands
// to on every frontend (TUI / Web / ACP). It tells the agent to run the named
// saved workflow via the workflow_run tool rather than authoring an inline
// script; any text the user typed after the slash is handed over so the agent can
// shape it into the workflow's `args` object. Keeping the wording here means all
// frontends stay in lockstep.
func SlashRunPrompt(name, userInput string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Run the saved workflow %q by calling the workflow_run tool with name=%q. "+
		"Do not write an inline script — run the saved workflow by name.", name, name)
	if s := strings.TrimSpace(userInput); s != "" {
		fmt.Fprintf(&b, "\n\nShape the workflow's `args` from this input: %s", s)
	}
	return b.String()
}
