package hooks

import (
	"regexp"
	"strings"
	"sync"
)

// toolAliases maps a real jcode tool name to alternative names it should also
// match. This lets a config written with Claude Code's tool names (Bash, Write,
// Edit, Read, …) match jcode's actual tools (execute, write, edit, read, …), so
// users can reuse hook configs across tools.
var toolAliases = map[string][]string{
	"execute": {"Bash", "run_shell"},
	"write":   {"Write"},
	"edit":    {"Edit"},
	"read":    {"Read"},
	"glob":    {"Glob"},
	"grep":    {"Grep"},
}

// matchCandidates returns the set of names a matcher is tested against for a
// given tool: its real name plus any aliases.
func matchCandidates(toolName string) []string {
	if toolName == "" {
		return []string{""}
	}
	names := []string{toolName}
	names = append(names, toolAliases[toolName]...)
	return names
}

var (
	reCacheMu sync.Mutex
	reCache   = map[string]*regexp.Regexp{}
)

// compileMatcher compiles (and caches) a matcher pattern. Invalid regexes return
// nil, which callers treat as "no match" rather than crashing.
func compileMatcher(pattern string) *regexp.Regexp {
	reCacheMu.Lock()
	defer reCacheMu.Unlock()
	if re, ok := reCache[pattern]; ok {
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}
	reCache[pattern] = re
	return re
}

// regexMeta are the characters that make a matcher part a regexp rather than a
// literal tool name.
const regexMeta = `.^$*+?()[]{}\|`

// matchesTool reports whether a group's matcher applies to the given tool.
//
//   - empty or "*" matches every tool (and non-tool events).
//   - otherwise the matcher is split on "|" into parts. A part with no regex
//     metacharacters is matched EXACTLY against the tool name and its aliases, so
//     "write" hooks the write tool and NOT todowrite/overwrite. A part containing
//     metacharacters is treated as a regexp (e.g. "mcp__.*", "^execute$").
//
// This exact-by-default rule avoids the classic footgun where an unanchored
// "write" regexp silently also matches the high-frequency todowrite tool.
func matchesTool(matcher, toolName string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	candidates := matchCandidates(toolName)
	for _, part := range strings.Split(matcher, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.ContainsAny(part, regexMeta) {
			re := compileMatcher(part)
			if re == nil {
				continue // invalid regex → no match, no panic
			}
			for _, name := range candidates {
				if re.MatchString(name) {
					return true
				}
			}
		} else {
			for _, name := range candidates {
				if name == part {
					return true
				}
			}
		}
	}
	return false
}
