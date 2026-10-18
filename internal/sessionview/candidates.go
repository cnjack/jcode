package sessionview

import (
	"context"
	"os"
	"sort"
	"time"

	"github.com/cnjack/jcode/internal/session"
)

const (
	defaultListLimit = 20
	maxListLimit     = 50
	// maxSearchCandidates bounds how many sessions one search may scan
	// (design §5.10). Newest-first ordering keeps the cap meaningful.
	maxSearchCandidates = 200
)

// candidate is one validated session index entry.
type candidate struct {
	meta session.SessionMeta
	path string
}

// candidates enumerates and validates the sessions of caller.Project.
//
// Every id comes from the on-disk index and is therefore untrusted: each one
// is pushed through sessionPath (ValidateSessionID + SessionsDir) before the
// candidate is accepted. Invalid ids are counted and logged and can never
// produce a filesystem read (design B2).
//
// excludeSelf removes the caller's own session (search); limit < 0 means no
// cap, limit > 0 truncates after sorting newest-first.
func (s *Service) candidates(ctx context.Context, c Caller, includeArchived, excludeSelf bool, limit int) (out []candidate, skipped, eligible int, err error) {
	if c.SessionID == "" {
		return nil, 0, 0, &viewError{sentinel: ErrNoCaller, reason: "no_caller"}
	}
	if c.Project == "" {
		return nil, 0, 0, noWorkspace()
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, err
	}
	metas, err := session.ListSessions(c.Project)
	if err != nil {
		s.logf("[session_view] error op=list_index project=%s err=%v", c.Project, err)
		return nil, 0, 0, &viewError{sentinel: ErrIO}
	}
	seen := make(map[string]struct{}, len(metas))
	for _, meta := range metas {
		if excludeSelf && meta.UUID == c.SessionID {
			continue
		}
		if meta.Archived && !includeArchived {
			continue
		}
		path, perr := sessionPath(meta.UUID)
		if perr != nil {
			skipped++
			s.logf("[session_view] skip target=%s reason=invalid_id", meta.UUID)
			continue
		}
		if _, dup := seen[meta.UUID]; dup {
			continue
		}
		seen[meta.UUID] = struct{}{}
		out = append(out, candidate{meta: meta, path: path})
	}
	sortCandidates(out)
	// Eligible reports the full eligible set; the scan cap below only bounds
	// how many of them a single search actually opens.
	eligible = len(out)
	if limit > 0 && len(out) > limit {
		s.logf("[session_view] candidate cap reached project=%s eligible=%d cap=%d", c.Project, len(out), limit)
		out = out[:limit]
	}
	return out, skipped, eligible, nil
}

// sortCandidates orders candidates newest first (UpdatedAt, falling back to
// StartTime) with UUID ascending as the deterministic tie-break.
func sortCandidates(cands []candidate) {
	sort.SliceStable(cands, func(i, j int) bool {
		ti, tj := metaTime(cands[i].meta), metaTime(cands[j].meta)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return cands[i].meta.UUID < cands[j].meta.UUID
	})
}

// metaTime parses the recency timestamp of a session, falling back to
// StartTime. Unparseable values sort as zero (oldest).
func metaTime(meta session.SessionMeta) time.Time {
	for _, raw := range []string{meta.UpdatedAt, meta.StartTime} {
		if raw == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return ts
		}
	}
	return time.Time{}
}

// refFor projects index metadata (plus an optional parsed session_start) into
// the model-visible SessionRef.
func (s *Service) refFor(meta session.SessionMeta, callerID string, start session.Entry) SessionRef {
	provider := meta.Provider
	if provider == "" {
		provider = start.Provider
	}
	model := meta.Model
	if model == "" {
		model = start.Model
	}
	project := meta.Project
	if project == "" {
		project = start.Project
	}
	updated := meta.UpdatedAt
	if updated == "" {
		updated = meta.StartTime
	}
	if updated == "" {
		updated = start.Timestamp
	}
	return SessionRef{
		ID:         meta.UUID,
		Title:      meta.Title,
		Project:    project,
		Provider:   provider,
		Model:      model,
		UpdatedAt:  updated,
		Archived:   meta.Archived,
		Automation: meta.AutomationID != "",
		IsCaller:   meta.UUID != "" && meta.UUID == callerID,
	}
}

// List implements session_list: every readable session in the caller's
// workspace, newest first, including the caller's own session (marked
// IsCaller) so the model can read the parts of its history that were compacted
// away (design N1).
func (s *Service) List(ctx context.Context, c Caller, o ListOptions) (ListResult, error) {
	var res ListResult
	cands, _, eligible, err := s.candidates(ctx, c, o.IncludeArchived, false, -1)
	if err != nil {
		return res, err
	}
	limit := o.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	res.Eligible = eligible
	shown := cands
	if len(shown) > limit {
		shown = shown[:limit]
	}
	for _, cand := range shown {
		ref := s.refFor(cand.meta, c.SessionID, session.Entry{})
		ref.SubagentTranscripts = s.countSubagentTranscripts(cand.meta.UUID)
		res.Sessions = append(res.Sessions, ref)
	}
	res.Shown = len(res.Sessions)
	res.Capped = res.Eligible > res.Shown
	s.logf("[session_view] list caller=%s project=%s eligible=%d shown=%d",
		c.SessionID, c.Project, res.Eligible, res.Shown)
	return res, nil
}

// ListOptions controls List.
type ListOptions struct {
	Limit           int
	IncludeArchived bool
}

// ListResult is the projection returned by List.
type ListResult struct {
	Sessions []SessionRef
	Eligible int
	Shown    int
	Capped   bool
}

// openCandidate opens one indexed candidate and re-checks the file's own
// session_start against the index id and the caller's project (design §5.3).
// The returned handle is rewound to offset 0 and is the SAME handle the caller
// must stream from (N5). opened reports whether os.Open succeeded; ok reports
// whether the file passed the re-check.
func (s *Service) openCandidate(cand candidate, project string) (f *os.File, opened, ok bool) {
	f, err := os.Open(cand.path)
	if err != nil {
		s.logf("[session_view] skip target=%s reason=open_failed err=%v", cand.meta.UUID, err)
		return nil, false, false
	}
	first, hasFirst, err := readFirstEntry(f)
	if err != nil || !hasFirst {
		s.logf("[session_view] skip target=%s reason=first_line_corrupt", cand.meta.UUID)
		_ = f.Close()
		return nil, true, false
	}
	if first.Type == session.EntrySessionStart {
		if first.UUID != "" && first.UUID != cand.meta.UUID {
			s.logf("[session_view] skip target=%s reason=start_uuid_mismatch", cand.meta.UUID)
			_ = f.Close()
			return nil, true, false
		}
		if first.Project != "" && first.Project != project {
			s.logf("[session_view] skip target=%s reason=project_mismatch", cand.meta.UUID)
			_ = f.Close()
			return nil, true, false
		}
	}
	if err := rewind(f); err != nil {
		s.logf("[session_view] error op=rewind target=%s err=%v", cand.meta.UUID, err)
		_ = f.Close()
		return nil, true, false
	}
	return f, true, true
}
