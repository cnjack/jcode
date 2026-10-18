package sessionview

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/cnjack/jcode/internal/session"
)

// SessionRef is the model-visible metadata of one session. It is a projection
// of the on-disk index plus (for ResolveTarget) the parsed session_start.
type SessionRef struct {
	ID                  string
	Title               string
	Project             string
	Provider            string
	Model               string
	UpdatedAt           string
	Archived            bool
	Automation          bool
	IsCaller            bool
	SubagentTranscripts int
}

// Target is an authorized, already-open session. File is positioned at an
// unspecified offset and is owned by the caller, who must Close it. The
// authorization re-check and every subsequent read share this one handle
// (design N5): a concurrent rename cannot swap the inode between the check and
// the read. Callers serve reads from offset 0.
type Target struct {
	Ref   SessionRef
	File  *os.File
	Start session.Entry // parsed session_start (zero for a legacy header)
}

// ResolveTarget authorizes a read of id ("" or the caller's own id means self)
// and returns the opened file. See design §5.3 for the full matrix.
//
// All denials return the single ErrSessionUnavailable sentinel; the concrete
// cause is log-only (Reason). The self branch runs through the same
// sessionPath/ValidateSessionID chokepoint as every other id (B2).
func (s *Service) ResolveTarget(ctx context.Context, c Caller, id string) (Target, error) {
	var target Target
	if err := ctx.Err(); err != nil {
		return target, err
	}
	if c.SessionID == "" {
		return target, &viewError{sentinel: ErrNoCaller, reason: "no_caller"}
	}
	self := id == "" || id == c.SessionID
	if self {
		// Binding correction: an empty id means "this session". It is resolved
		// to the caller's id and follows the identical validation/read path; it
		// never produces ErrSessionUnavailable merely for being empty.
		id = c.SessionID
	}
	if !self && c.Project == "" {
		// Matrix: with no recorded workspace only self is readable.
		return target, noWorkspace()
	}

	path, err := sessionPath(id)
	if err != nil {
		s.logDeny(c, id, err, "")
		return target, err
	}

	// The index is only a source of candidate ids and metadata. Any index
	// failure is classified exactly once here: read/unmarshal/IO => ErrIO,
	// duplicates/ambiguity => ErrSessionUnavailable (never ErrIO, which would
	// turn the index into an existence oracle).
	meta, err := session.FindSessionMeta(id)
	if err != nil {
		if indexIOError(err) {
			s.logf("[session_view] error op=index target=%s err=%v", id, err)
			return target, &viewError{sentinel: ErrIO}
		}
		s.logDeny(c, id, unavailable("index_error"), "")
		return target, unavailable("index_error")
	}
	if meta == nil {
		s.logDeny(c, id, unavailable("not_indexed"), "")
		return target, unavailable("not_indexed")
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.logDeny(c, id, unavailable("missing_file"), meta.Project)
			return target, unavailable("missing_file")
		}
		s.logf("[session_view] error op=open target=%s err=%v", id, err)
		return target, &viewError{sentinel: ErrIO}
	}
	// From here on the handle is either closed on error or handed to the caller.
	keep := false
	defer func() {
		if !keep {
			_ = f.Close()
		}
	}()

	first, hasFirst, err := readFirstEntry(f)
	if err != nil {
		var syn *json.SyntaxError
		var typ *json.UnmarshalTypeError
		if errors.As(err, &syn) || errors.As(err, &typ) {
			s.logDeny(c, id, unavailable("first_line_corrupt"), meta.Project)
			return target, unavailable("first_line_corrupt")
		}
		s.logf("[session_view] error op=read_start target=%s err=%v", id, err)
		return target, &viewError{sentinel: ErrIO}
	}
	if !hasFirst {
		s.logDeny(c, id, unavailable("first_line_corrupt"), meta.Project)
		return target, unavailable("first_line_corrupt")
	}

	var start session.Entry
	observed := ""
	if first.Type == session.EntrySessionStart {
		start = first
		observed = first.Project
		if first.UUID != "" && first.UUID != id {
			s.logDeny(c, id, unavailable("start_uuid_mismatch"), first.Project)
			return target, unavailable("start_uuid_mismatch")
		}
	} else {
		// Legacy / headerless file: fall back to the index project.
		s.logf("[session_view] target=%s reason=no_session_start", id)
	}
	if observed == "" {
		observed = meta.Project
	}

	if !self {
		if observed != c.Project {
			s.logDeny(c, id, unavailable("project_mismatch"), observed)
			return target, unavailable("project_mismatch")
		}
		// Both sources present and disagreeing: fail closed.
		if meta.Project != "" && start.Project != "" && meta.Project != start.Project {
			s.logDeny(c, id, unavailable("index_mismatch"), observed)
			return target, unavailable("index_mismatch")
		}
	}

	// Rewind the SAME handle for the caller's streaming read (N5).
	if err := rewind(f); err != nil {
		s.logf("[session_view] error op=rewind target=%s err=%v", id, err)
		return target, &viewError{sentinel: ErrIO}
	}

	ref := s.refFor(*meta, c.SessionID, start)
	ref.SubagentTranscripts = s.countSubagentTranscripts(id)
	keep = true
	return Target{Ref: ref, File: f, Start: start}, nil
}

// logDeny records the log-only denial detail from design §5.3. The reason is
// never rendered to the model.
func (s *Service) logDeny(c Caller, target string, err error, observed string) {
	s.logf("[session_view] deny caller=%s target=%s reason=%s caller_project=%s observed_project=%s",
		c.SessionID, target, Reason(err), c.Project, observed)
}

// indexIOError reports whether an error from session.FindSessionMeta is a real
// IO/parse failure of the shared index (as opposed to a duplicate entry, which
// FindSessionMeta also reports as an error).
func indexIOError(err error) bool {
	var pathErr *fs.PathError
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	if errors.As(err, &pathErr) || errors.As(err, &syn) || errors.As(err, &typ) {
		return true
	}
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, io.ErrUnexpectedEOF)
}
