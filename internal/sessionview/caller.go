// Package sessionview is the read-only service layer behind the cross-session
// view tools (session_list / session_search / session_read).
//
// The package owns authorization (project-scoped fail-closed reads), tolerant
// JSONL parsing, literal search, entry-addressed reads with output budgets,
// and lineage counts. It deliberately contains no model-facing copy: the tool
// layer in internal/tools renders every string. All diagnostics go to
// config.Logger(); nothing is written to stdout/stderr.
//
// Path construction is centralized in paths.go: every session id — including
// the caller's own id and every id sourced from the on-disk index — is first
// validated with session.ValidateSessionID before a path is built (design B2).
package sessionview

import (
	"errors"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// Caller is the identity of the calling session. Both fields are sourced from
// the runtime recorder (Env.SessionCallerFn); the model cannot influence them.
// An empty field means "unknown".
type Caller struct {
	SessionID string
	Project   string
}

// Config tunes the service. Zero or negative values fall back to the defaults
// in the design (§5.8): 20 results, 4 KiB per entry, 16 KiB per call, 3 s.
type Config struct {
	MaxResults     int
	MaxEntryBytes  int
	MaxOutputBytes int
	TimeBudget     time.Duration
}

const (
	defaultMaxResults     = 20
	defaultMaxEntryBytes  = 4096
	defaultMaxOutputBytes = 16384
	defaultTimeBudget     = 3 * time.Second
	hardMaxResults        = 200
)

// Service is the read-only cross-session view service. It is immutable after
// New and therefore safe for concurrent use.
type Service struct {
	cfg  Config
	now  func() time.Time
	logf func(format string, args ...any)
}

// New returns a Service with the design defaults applied to unset fields.
func New(cfg Config) *Service {
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = defaultMaxResults
	}
	if cfg.MaxResults > hardMaxResults {
		cfg.MaxResults = hardMaxResults
	}
	if cfg.MaxEntryBytes <= 0 {
		cfg.MaxEntryBytes = defaultMaxEntryBytes
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	if cfg.TimeBudget <= 0 {
		cfg.TimeBudget = defaultTimeBudget
	}
	return &Service{
		cfg: cfg,
		now: time.Now,
		logf: func(format string, args ...any) {
			config.Logger().Printf(format, args...)
		},
	}
}

// Error sentinels. The tool layer classifies service failures with errors.Is
// and renders fixed model-facing copy; only ErrInvalidQuery / ErrInvalidRange
// carry a self-correcting detail string as their Error() text.
var (
	// ErrSessionUnavailable covers "does not exist", "not authorized", "stale
	// index", and "invalid id" with one indistinguishable sentinel.
	ErrSessionUnavailable = errors.New("session unavailable")
	// ErrNoCaller means the calling session has no recorded identity.
	ErrNoCaller = errors.New("no caller identity")
	// ErrNoWorkspace means the calling session has no recorded project.
	ErrNoWorkspace = errors.New("no caller workspace")
	// ErrInvalidQuery means the search query is unusable.
	ErrInvalidQuery = errors.New("invalid query")
	// ErrInvalidRange means the read range is unusable.
	ErrInvalidRange = errors.New("invalid range")
	// ErrIO means a directory, index, or session file could not be read.
	ErrIO = errors.New("session IO failure")
)

// viewError attaches a log-only reason code and/or a model-safe detail string
// to one of the sentinels. Error() never contains the target id or project:
// the detail is only set for input-validation errors where self-correction is
// safe and the text is fixed copy from the design.
type viewError struct {
	sentinel error
	detail   string
	reason   string
}

func (e *viewError) Error() string {
	if e.detail != "" {
		return e.detail
	}
	return e.sentinel.Error()
}

func (e *viewError) Unwrap() error { return e.sentinel }

// Reason returns the log-only reason detail for an authorization/IO failure.
// It must never be rendered to the model: it can distinguish "not authorized"
// from "does not exist". Returns "" when there is no reason attached.
func Reason(err error) string {
	var ve *viewError
	if errors.As(err, &ve) {
		return ve.reason
	}
	return ""
}

func unavailable(reason string) error {
	return &viewError{sentinel: ErrSessionUnavailable, reason: reason}
}

func noWorkspace() error {
	return &viewError{sentinel: ErrNoWorkspace, reason: "empty_project"}
}

func invalidQuery(detail string) error {
	return &viewError{sentinel: ErrInvalidQuery, detail: detail, reason: "invalid_query"}
}

func invalidRange(detail string) error {
	return &viewError{sentinel: ErrInvalidRange, detail: detail, reason: "invalid_range"}
}
