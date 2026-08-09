package session

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// SessionTool identifies historical provider-backed task preferences in
// durable journals. Both identifiers remain parseable for replay compatibility,
// but neither is a current product configuration surface.
type SessionTool string

const (
	SessionToolImageGeneration SessionTool = "image_generation"
	SessionToolWebSearch       SessionTool = "web_search"
)

// sessionToolOverrideStoreMu serializes process-local readers/CAS writers even
// when the same session was accidentally opened by more than one Recorder.
// Each operation still reloads the JSONL projection, so one recorder's cache
// can never authorize a duplicate revision from another recorder.
var sessionToolOverrideStoreMu sync.Mutex

// SupportedSessionTools is empty because provider tools are no longer
// user-selectable per session. Retained so older integrations compile while
// discovering that the product exposes no configurable session tools.
func SupportedSessionTools() []SessionTool {
	return nil
}

// IsConfigurableSessionTool always returns false. ParseSessionTool and the
// journal replay/CAS methods remain available only for historical records.
func IsConfigurableSessionTool(SessionTool) bool {
	return false
}

// ParseSessionTool validates the exact persisted/API identifier.
func ParseSessionTool(raw string) (SessionTool, error) {
	tool := SessionTool(raw)
	switch tool {
	case SessionToolImageGeneration, SessionToolWebSearch:
		return tool, nil
	default:
		return "", fmt.Errorf("unsupported session tool %q", raw)
	}
}

// SessionToolOverride is the historical durable preference projection. It is
// parsed for compatibility and does not gate current runtime availability.
type SessionToolOverride struct {
	Tool      SessionTool `json:"tool"`
	Persisted bool        `json:"persisted"`
	Revision  uint64      `json:"revision"`
}

var ErrSessionToolOverrideRevision = errors.New("session tool override revision conflict")

// SessionToolOverrideRevisionError reports a failed compare-and-swap without
// losing the actual current revision needed by a client to resynchronize.
type SessionToolOverrideRevisionError struct {
	Tool     SessionTool
	Expected uint64
	Actual   uint64
}

func (e *SessionToolOverrideRevisionError) Error() string {
	return fmt.Sprintf("%s for %s: expected revision %d, current revision %d",
		ErrSessionToolOverrideRevision, e.Tool, e.Expected, e.Actual)
}

func (e *SessionToolOverrideRevisionError) Unwrap() error {
	return ErrSessionToolOverrideRevision
}

// ReplaySessionToolOverrides projects the latest valid revision for each
// allowlisted tool. The greatest revision wins rather than line order, so a
// duplicated or out-of-order line cannot roll state backwards.
func ReplaySessionToolOverrides(entries []Entry) map[SessionTool]SessionToolOverride {
	latest := make(map[SessionTool]SessionToolOverride, 2)
	for _, entry := range entries {
		if entry.Type != EntrySessionToolOverride || entry.SessionToolOverrideRevision == 0 {
			continue
		}
		tool, err := ParseSessionTool(entry.SessionToolOverrideTool)
		if err != nil {
			continue
		}
		current := latest[tool]
		if entry.SessionToolOverrideRevision <= current.Revision {
			continue
		}
		latest[tool] = SessionToolOverride{
			Tool:      tool,
			Persisted: entry.SessionToolOverridePersisted,
			Revision:  entry.SessionToolOverrideRevision,
		}
	}
	return latest
}

// LoadSessionToolOverrides replays the latest persisted override for a session.
func LoadSessionToolOverrides(id string) (map[SessionTool]SessionToolOverride, error) {
	sessionToolOverrideStoreMu.Lock()
	defer sessionToolOverrideStoreMu.Unlock()
	entries, err := loadSecuritySession(id)
	if err != nil {
		return nil, err
	}
	return ReplaySessionToolOverrides(entries), nil
}

// SessionToolOverrides returns a snapshot for this recorder. A brand-new
// recorder has an empty snapshot without forcing creation of a session file.
func (r *Recorder) SessionToolOverrides() (map[SessionTool]SessionToolOverride, error) {
	sessionToolOverrideStoreMu.Lock()
	defer sessionToolOverrideStoreMu.Unlock()
	sessionID := r.UUID()
	exists, err := sessionFileExists(sessionID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return make(map[SessionTool]SessionToolOverride), nil
	}
	lock, err := acquireSessionSecurityLock(sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.release() }()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.uuid != sessionID {
		return nil, fmt.Errorf("session changed while loading tool overrides")
	}
	r.toolOverridesLoaded = false
	if err := r.loadSessionToolOverridesLocked(); err != nil {
		return nil, err
	}
	return cloneSessionToolOverrides(r.toolOverrides), nil
}

// CompareAndSwapSessionToolOverride persists one monotonic revision. The
// in-memory replay cache is published only after write+fsync succeeds.
func (r *Recorder) CompareAndSwapSessionToolOverride(
	tool SessionTool,
	persisted bool,
	expectedRevision uint64,
) (SessionToolOverride, error) {
	if _, err := ParseSessionTool(string(tool)); err != nil {
		return SessionToolOverride{}, err
	}
	sessionToolOverrideStoreMu.Lock()
	defer sessionToolOverrideStoreMu.Unlock()
	sessionID := r.UUID()
	lock, err := acquireSessionSecurityLock(sessionID)
	if err != nil {
		return SessionToolOverride{}, err
	}
	defer func() { _ = lock.release() }()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.uuid != sessionID {
		return SessionToolOverride{}, fmt.Errorf("session changed while updating tool override")
	}
	r.toolOverridesLoaded = false
	if err := r.loadSessionToolOverridesLocked(); err != nil {
		return SessionToolOverride{}, err
	}
	current := r.toolOverrides[tool]
	if current.Revision != expectedRevision {
		return current, &SessionToolOverrideRevisionError{
			Tool: tool, Expected: expectedRevision, Actual: current.Revision,
		}
	}
	next := SessionToolOverride{Tool: tool, Persisted: persisted, Revision: expectedRevision + 1}
	if err := r.writeSecurityEntryLocked(Entry{
		Type:                         EntrySessionToolOverride,
		SessionToolOverrideTool:      string(tool),
		SessionToolOverridePersisted: persisted,
		SessionToolOverrideRevision:  next.Revision,
	}); err != nil {
		return current, fmt.Errorf("persist session tool override %s: %w", tool, err)
	}
	if r.toolOverrides == nil {
		r.toolOverrides = make(map[SessionTool]SessionToolOverride)
	}
	r.toolOverrides[tool] = next
	return next, nil
}

func (r *Recorder) loadSessionToolOverridesLocked() error {
	if r.toolOverridesLoaded {
		return nil
	}
	entries, err := loadSecuritySession(r.uuid)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		entries = nil
	} else if r.file == nil {
		// Another Recorder may have created this client-provided task after
		// SetUUID checked the filesystem. Any future append must open it rather
		// than attempting O_EXCL creation.
		r.resuming = true
	}
	r.toolOverrides = ReplaySessionToolOverrides(entries)
	r.toolOverridesLoaded = true
	return nil
}

func cloneSessionToolOverrides(
	source map[SessionTool]SessionToolOverride,
) map[SessionTool]SessionToolOverride {
	result := make(map[SessionTool]SessionToolOverride, len(source))
	for tool, override := range source {
		result[tool] = override
	}
	return result
}
