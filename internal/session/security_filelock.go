package session

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cnjack/jcode/internal/config"
)

func sessionFileExists(sessionID string) (bool, error) {
	if err := ValidateSessionID(sessionID); err != nil {
		return false, err
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(dir, sessionID+".json"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// acquireSessionSecurityLock serializes security-policy CAS, billable journal
// appends, and transcript rewrites across all jcode processes using a session.
// The OS releases the advisory lock if a process crashes.
func acquireSessionSecurityLock(sessionID string) (*securityFileLock, error) {
	if err := ValidateSessionID(sessionID); err != nil {
		return nil, err
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return nil, err
	}
	if err := ensurePrivateSessionDir(dir); err != nil {
		return nil, fmt.Errorf("secure sessions directory: %w", err)
	}
	lock, err := acquireSecurityFileLock(filepath.Join(dir, "."+sessionID+".security.lock"))
	if err != nil {
		return nil, fmt.Errorf("lock session security journal: %w", err)
	}
	return lock, nil
}
