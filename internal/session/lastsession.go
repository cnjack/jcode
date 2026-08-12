package session

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cnjack/jcode/internal/config"
)

// lastSessionFile is the on-disk structure of last_session.json: the most
// recently foregrounded session per project, so a web/desktop client can
// return to the conversation that was open before a restart.
type lastSessionFile struct {
	Projects      map[string]string `json:"projects"`                 // project path → session uuid
	RecentProject string            `json:"recent_project,omitempty"` // last foregrounded project across workspaces
	RecentSession string            `json:"recent_session,omitempty"` // last foregrounded session across workspaces
}

func lastSessionPath() (string, error) {
	dir, err := config.SessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "last_session.json"), nil
}

// SaveLastSession records id as the last foregrounded session for project.
// Best-effort: persistence must never break session switching, and callers
// run outside any engine lock (file I/O).
func SaveLastSession(project, id string) {
	if project == "" || id == "" || ValidateSessionID(id) != nil {
		return
	}
	indexMu.Lock()
	defer indexMu.Unlock()

	p, err := lastSessionPath()
	if err != nil {
		return
	}
	var f lastSessionFile
	if data, readErr := os.ReadFile(p); readErr == nil {
		_ = json.Unmarshal(data, &f) // corrupt file → start fresh
	}
	if f.Projects == nil {
		f.Projects = map[string]string{}
	}
	if f.Projects[project] == id && f.RecentProject == project && f.RecentSession == id {
		return
	}
	f.Projects[project] = id
	f.RecentProject = project
	f.RecentSession = id

	if err := ensurePrivateSessionDir(filepath.Dir(p)); err != nil {
		return
	}
	data, err := json.Marshal(&f)
	if err != nil {
		return
	}
	// tmp + rename (same pattern as the session index) so a crash mid-write
	// never leaves a truncated file.
	tmp := p + ".tmp"
	if err := writePrivateSessionFile(tmp, data); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}

// LoadLastSession returns the last foregrounded session uuid for project, or
// "" when none is recorded — or when the recorded session no longer exists on
// disk (deleted, or a "new chat" that was never written), so callers fall
// back to a fresh session instead of resurrecting a stale id.
func LoadLastSession(project string) string {
	if project == "" {
		return ""
	}
	p, err := lastSessionPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var f lastSessionFile
	if err := json.Unmarshal(data, &f); err != nil {
		return ""
	}
	return validateLastSessionID(f.Projects[project])
}

// LoadMostRecentSession returns the last foregrounded project/session across
// all workspaces. Desktop uses this on a cold start because its sidecar always
// boots from a local directory and therefore cannot infer that the previously
// focused workspace was SSH or Docker. Project-scoped web startup continues to
// use LoadLastSession.
func LoadMostRecentSession() (project, id string) {
	p, err := lastSessionPath()
	if err != nil {
		return "", ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", ""
	}
	var f lastSessionFile
	if err := json.Unmarshal(data, &f); err != nil {
		return "", ""
	}
	// One-time compatibility for last_session.json files written before the
	// global pointer existed: choose the most recently written transcript among
	// the already-recorded per-project foreground entries. The next focus saves
	// an exact recent pointer, so this approximation is not used again.
	if f.RecentProject == "" || f.RecentSession == "" {
		return inferMostRecentLegacySession(f.Projects)
	}
	id = validateLastSessionID(f.RecentSession)
	if id == "" {
		return "", ""
	}
	return f.RecentProject, id
}

func inferMostRecentLegacySession(projects map[string]string) (project, id string) {
	dir, err := config.SessionsDir()
	if err != nil {
		return "", ""
	}
	var (
		latest int64
		have   bool
	)
	for candidateProject, candidateID := range projects {
		if ValidateSessionID(candidateID) != nil {
			continue
		}
		info, statErr := os.Stat(filepath.Join(dir, candidateID+".json"))
		if statErr != nil {
			continue
		}
		stamp := info.ModTime().UnixNano()
		if have && stamp <= latest {
			continue
		}
		project, id, latest, have = candidateProject, candidateID, stamp, true
	}
	return project, id
}

func validateLastSessionID(id string) string {
	if id == "" || ValidateSessionID(id) != nil {
		return ""
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, id+".json")); err != nil {
		return ""
	}
	return id
}
