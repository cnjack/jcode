package sessionview

import (
	"os"
	"path/filepath"
	"strings"
)

// countSubagentTranscripts counts the agent-*.jsonl files under
// <SessionsDir>/<id>/subagents. It only ever reads directory names: subagent
// transcript content is never opened (design §5.7).
func (s *Service) countSubagentTranscripts(id string) int {
	dir, err := sessionDir(id)
	if err != nil {
		// Invalid ids never reach the filesystem (B2).
		return 0
	}
	entries, err := os.ReadDir(filepath.Join(dir, "subagents"))
	if err != nil {
		if !os.IsNotExist(err) {
			s.logf("[session_view] error op=read_subagents target=%s err=%v", id, err)
		}
		return 0
	}
	n := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl") {
			n++
		}
	}
	return n
}
