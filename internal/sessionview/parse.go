package sessionview

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/cnjack/jcode/internal/session"
)

const (
	// readBufferSize is the bufio.Reader size used for streaming scans.
	readBufferSize = 64 << 10
	// maxLineBytes bounds a single JSONL line. Oversized lines are skipped and
	// counted as corrupt so one bad write can never stall a scan.
	maxLineBytes = 8 << 20
	// maxScanFileBytes bounds one file considered by session_search.
	maxScanFileBytes = 64 << 20
)

// rawLine is one physical line returned by readRawLine.
type rawLine struct {
	data       []byte
	terminated bool // a trailing '\n' was seen
	tooLong    bool // line exceeded maxLineBytes and was discarded
}

// readRawLine reads one '\n'-terminated line, tolerating lines larger than the
// bufio buffer. An unterminated final fragment is returned with terminated
// false; the caller drops it (torn tail write) and reports Torn.
func readRawLine(br *bufio.Reader) (rawLine, error) {
	var buf []byte
	for {
		frag, err := br.ReadSlice('\n')
		switch {
		case err == nil:
			if len(buf)+len(frag) > maxLineBytes {
				return rawLine{tooLong: true, terminated: true}, nil
			}
			buf = append(buf, frag...)
			return rawLine{data: buf, terminated: true}, nil
		case errors.Is(err, bufio.ErrBufferFull):
			if len(buf)+len(frag) > maxLineBytes {
				// Discard the rest of this oversized line so the next read
				// starts at a line boundary.
				for {
					_, derr := br.ReadSlice('\n')
					switch {
					case errors.Is(derr, bufio.ErrBufferFull):
						continue
					case derr == nil:
						return rawLine{tooLong: true, terminated: true}, nil
					case errors.Is(derr, io.EOF):
						return rawLine{tooLong: true}, nil
					default:
						return rawLine{}, derr
					}
				}
			}
			buf = append(buf, frag...)
		case errors.Is(err, io.EOF):
			if len(buf)+len(frag) == 0 {
				return rawLine{}, io.EOF
			}
			if len(buf)+len(frag) > maxLineBytes {
				return rawLine{tooLong: true}, nil
			}
			buf = append(buf, frag...)
			return rawLine{data: buf}, nil
		default:
			return rawLine{}, err
		}
	}
}

// parseStats is the outcome of a tolerant streaming scan.
type parseStats struct {
	Total    int  // successfully parsed entries (1-based seq ordinals)
	Corrupt  int  // lines skipped because they were not parseable
	Torn     bool // last line had no trailing newline and was dropped
	Start    session.Entry
	HasStart bool // Start is a session_start entry (always seq 1 when present)
}

// scanOptions tunes a streaming scan.
type scanOptions struct {
	// prefilter, when non-nil, runs on the raw JSONL bytes before decoding. A
	// rejected line is still counted for seq ordinals if it is syntactically
	// valid JSON, so entry numbers stay identical to a full decode. The
	// prefilter is only ever an optimization: callers must re-verify matches on
	// the decoded text.
	prefilter func(line []byte) bool
}

// scanFile streams a JSONL session file from its current offset. visit is
// called for every successfully parsed entry with its 1-based ordinal (seq)
// and returns false to stop the scan early.
//
// Corruption policy matches session.LoadSession: empty lines are ignored,
// unparseable lines are counted and skipped, and a trailing fragment without a
// newline is dropped (Torn) because the writer appends complete lines.
func (s *Service) scanFile(ctx context.Context, f *os.File, opts scanOptions, visit func(seq int, e session.Entry) bool) (parseStats, error) {
	br := bufio.NewReaderSize(f, readBufferSize)
	var stats parseStats
	seq := 0
	lines := 0
	for {
		lines++
		if lines%256 == 0 && ctx.Err() != nil {
			// Stop cleanly; the caller distinguishes budget from cancel.
			stats.Total = seq
			return stats, nil
		}
		rl, err := readRawLine(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return stats, err
		}
		if rl.tooLong {
			stats.Corrupt++
			s.logf("[session_view] skip oversized line (corrupt=%d)", stats.Corrupt)
			continue
		}
		if !rl.terminated {
			// Torn tail write: the writer appends whole lines, so a fragment is
			// never a complete entry. Drop it and report it.
			stats.Torn = true
			break
		}
		line := bytes.TrimSpace(rl.data)
		if len(line) == 0 {
			continue
		}
		if opts.prefilter != nil && !opts.prefilter(line) {
			// Count this line exactly as the decode path would (Unmarshal into
			// Entry, not mere syntactic validity), so a JSON-valid non-object
			// line stays corrupt here and session_search seq numbers keep
			// addressing the same entries as session_read.
			var probe session.Entry
			if err := json.Unmarshal(line, &probe); err == nil {
				seq++ // parsed entry that simply did not contain the needle
			} else {
				stats.Corrupt++
			}
			continue
		}
		var e session.Entry
		if err := json.Unmarshal(line, &e); err != nil {
			stats.Corrupt++
			s.logf("[session_view] corrupted line skipped (corrupt=%d): %v", stats.Corrupt, err)
			continue
		}
		seq++
		if seq == 1 && e.Type == session.EntrySessionStart {
			stats.Start = e
			stats.HasStart = true
		}
		stats.Total = seq
		if !visit(seq, e) {
			return stats, nil
		}
	}
	stats.Total = seq
	return stats, nil
}

// readFirstEntry parses the first non-empty physical line of f without
// rewinding. It is used by ResolveTarget for the same-handle project
// re-check (design N5); callers that continue reading must Seek(0,0) first.
//
// An empty file or a file with only blank lines returns ok=false. A line that
// is valid JSON but not a session_start is returned as-is (the caller falls
// back to the index project). A line that does not parse returns an error.
func readFirstEntry(f *os.File) (session.Entry, bool, error) {
	br := bufio.NewReaderSize(f, readBufferSize)
	for i := 0; i < 8; i++ {
		rl, err := readRawLine(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return session.Entry{}, false, nil
			}
			return session.Entry{}, false, err
		}
		if rl.tooLong {
			return session.Entry{}, false, nil
		}
		line := bytes.TrimSpace(rl.data)
		if len(line) == 0 {
			continue
		}
		var e session.Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return session.Entry{}, false, err
		}
		return e, true, nil
	}
	return session.Entry{}, false, nil
}

// rewind moves f back to the start so the post-authorization read reuses the
// exact same open file handle (design N5).
func rewind(f *os.File) error {
	_, err := f.Seek(0, io.SeekStart)
	return err
}
