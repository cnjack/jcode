package sessionview

import (
	"path/filepath"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
)

// sessionDir and sessionPath are the ONLY functions allowed to build paths
// under the sessions directory (design B2). Every id — model supplied, index
// supplied, or the caller's own self id — passes through ValidateSessionID
// first, so a poisoned index entry ("../../x", "a/b", "", "..") can never
// escape the sessions directory.
func sessionDir(id string) (string, error) {
	if err := session.ValidateSessionID(id); err != nil {
		return "", unavailable("invalid_id")
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return "", &viewError{sentinel: ErrIO}
	}
	return filepath.Join(dir, id), nil
}

func sessionPath(id string) (string, error) {
	dir, err := sessionDir(id)
	if err != nil {
		return "", err
	}
	return dir + ".json", nil
}
