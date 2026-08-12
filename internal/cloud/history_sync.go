package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
)

const (
	historyProjectionVersion = 1
	historyUploadBatchSize   = 50
	// The current cloud clients fetch at most 1000 durable events when a
	// conversation is opened. Do not claim a successful full backfill when the
	// existing client could only render a prefix of it.
	historyProjectionLimit = 1000
	historySyncFile        = "cloud-history.json"
)

type projectedHistoryEvent struct {
	Kind    string
	Payload json.RawMessage
}

// historySyncRecord is the local crash-recovery cursor for the one-time
// transcript projection. The cloud protocol is idempotent by (session, seq),
// so a restarted connector may safely continue at the server high-water mark
// only when the exact same projection is still on disk.
type historySyncRecord struct {
	ProjectionVersion int    `json:"projection_version"`
	ProjectionHash    string `json:"projection_hash"`
	EventCount        int64  `json:"event_count"`
	NextSeq           int64  `json:"next_seq"`
	KeyGen            int    `json:"key_gen,omitempty"`
	Complete          bool   `json:"complete,omitempty"`
}

type historySyncLedger struct {
	Sessions map[string]historySyncRecord `json:"sessions"`
}

func historySyncPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cloud history path: %w", err)
	}
	return filepath.Join(home, ".jcode", historySyncFile), nil
}

func loadHistorySyncLedger(path string) (*historySyncLedger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &historySyncLedger{Sessions: make(map[string]historySyncRecord)}, nil
		}
		return nil, fmt.Errorf("read cloud history ledger: %w", err)
	}
	var ledger historySyncLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, fmt.Errorf("parse cloud history ledger: %w", err)
	}
	if ledger.Sessions == nil {
		ledger.Sessions = make(map[string]historySyncRecord)
	}
	return &ledger, nil
}

func saveHistorySyncLedger(path string, ledger *historySyncLedger) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create cloud history directory: %w", err)
	}
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cloud history ledger: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".cloud-history.tmp-*")
	if err != nil {
		return fmt.Errorf("create cloud history ledger temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure cloud history ledger: %w", err)
	}
	if n, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write cloud history ledger: %w", err)
	} else if n != len(data) {
		return fmt.Errorf("write cloud history ledger: %w", io.ErrShortWrite)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync cloud history ledger: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cloud history ledger: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace cloud history ledger: %w", err)
	}
	return nil
}

// projectSessionHistory translates the local JSONL source of truth into the
// durable event vocabulary already understood by the current cloud clients.
// It deliberately excludes streaming deltas and opaque/non-renderable records.
func projectSessionHistory(sessionID string, entries []session.Entry) ([]projectedHistoryEvent, error) {
	events := make([]projectedHistoryEvent, 0, len(entries))
	appendEvent := func(kind string, data map[string]any) error {
		payload, err := json.Marshal(map[string]any{
			"type":    kind,
			"task_id": sessionID,
			"data":    data,
		})
		if err != nil {
			return fmt.Errorf("marshal %s history event: %w", kind, err)
		}
		events = append(events, projectedHistoryEvent{Kind: kind, Payload: payload})
		if len(events) > historyProjectionLimit {
			return fmt.Errorf("projected history has more than %d renderable events", historyProjectionLimit)
		}
		return nil
	}

	for _, entry := range entries {
		var kind string
		var data map[string]any
		switch entry.Type {
		case session.EntryUser:
			if entry.Content == "" {
				continue
			}
			kind = "user_message"
			data = map[string]any{"content": entry.Content}
		case session.EntryAssistant:
			if entry.Content == "" {
				continue
			}
			kind = "agent_message"
			data = map[string]any{"text": entry.Content}
		case session.EntryToolCall:
			data = map[string]any{
				"name":         entry.Name,
				"args":         entry.Args,
				"tool_call_id": entry.ToolCallID,
			}
			if entry.BatchID != "" {
				data["batch_id"] = entry.BatchID
				data["batch_index"] = entry.BatchIndex
				data["batch_size"] = entry.BatchSize
			}
			kind = "tool_call"
		case session.EntryToolResult:
			data = map[string]any{
				"name":         entry.Name,
				"output":       entry.Output,
				"tool_call_id": entry.ToolCallID,
			}
			if entry.Error != "" {
				data["error"] = entry.Error
			}
			if entry.Denied {
				data["denied"] = true
			}
			if entry.DurationMs > 0 {
				data["duration_ms"] = entry.DurationMs
			}
			kind = "tool_result"
		case session.EntryModeChange:
			if entry.Mode == "" {
				continue
			}
			kind = "mode_changed"
			data = map[string]any{"mode": entry.Mode}
		case session.EntryGoalUpdate:
			kind = "goal_update"
			data = map[string]any{
				"objective":   entry.GoalObjective,
				"status":      entry.GoalStatus,
				"tokens_used": entry.GoalTokensUsed,
				"created_at":  entry.GoalCreatedAt,
				"updated_at":  entry.GoalUpdatedAt,
			}
		case session.EntrySubagentStart, session.EntrySubagentAsync:
			kind = "subagent_event"
			data = map[string]any{
				"name":       entry.SubagentName,
				"agent_type": entry.SubagentType,
				"done":       false,
			}
		case session.EntrySubagentResult:
			kind = "subagent_event"
			data = map[string]any{
				"name":   entry.SubagentName,
				"done":   true,
				"result": entry.Output,
				"error":  entry.Error,
			}
		default:
			continue
		}
		if err := appendEvent(kind, data); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func historyProjectionHash(events []projectedHistoryEvent) string {
	h := sha256.New()
	for _, event := range events {
		_, _ = h.Write([]byte(event.Kind))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(event.Payload)
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// loadSessionHistoryStrict differs intentionally from the interactive replay
// loader: a malformed JSONL line must stop a cloud backfill instead of silently
// publishing a transcript with a permanent hole and marking its ledger done.
func loadSessionHistoryStrict(sessionID string) ([]session.Entry, error) {
	if err := session.ValidateSessionID(sessionID); err != nil {
		return nil, err
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, sessionID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session history %s: %w", sessionID, err)
	}
	entries := make([]session.Entry, 0)
	for lineNo, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		var entry session.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parse session history %s line %d: %w", sessionID, lineNo+1, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (c *Connector) strictSealHistory(payload json.RawMessage) (json.RawMessage, int, error) {
	cipher := c.cipherSnapshot()
	if cipher == nil || c.cfg.CipherDisabled {
		return payload, 0, nil
	}
	sealed, err := cipher.Seal(payload)
	if err != nil {
		return nil, cipher.KeyGen(), fmt.Errorf("seal history event: %w", err)
	}
	return sealed, cipher.KeyGen(), nil
}

// backfillSessionHistory uploads a stable prefix projection when the cloud has
// no history yet. A matching in-progress ledger is the only case in which a
// non-zero server cursor may resume; unrelated legacy live events are left
// untouched because the current protocol cannot prepend history safely.
func (c *Connector) backfillSessionHistory(ctx context.Context, sessionID string, serverLastSeq int64) (int64, error) {
	loadFn := c.cfg.LoadSessionFn
	if loadFn == nil {
		loadFn = loadSessionHistoryStrict
	}
	entries, err := loadFn(sessionID)
	if err != nil {
		// Metadata can legitimately exist before the first recorded message.
		// Keep the ordinary session sync successful and try again later.
		return serverLastSeq, nil
	}
	events, err := projectSessionHistory(sessionID, entries)
	if err != nil {
		return serverLastSeq, err
	}
	hash := historyProjectionHash(events)
	path := c.cfg.HistorySyncPath
	if path == "" {
		path, err = historySyncPath()
		if err != nil {
			return serverLastSeq, err
		}
	}
	ledger, err := loadHistorySyncLedger(path)
	if err != nil {
		return serverLastSeq, err
	}

	keyGen := 0
	if cipher := c.cipherSnapshot(); cipher != nil && !c.cfg.CipherDisabled {
		keyGen = cipher.KeyGen()
	}
	record, recorded := ledger.Sessions[sessionID]
	matches := recorded &&
		record.ProjectionVersion == historyProjectionVersion &&
		record.ProjectionHash == hash &&
		record.EventCount == int64(len(events)) &&
		record.KeyGen == keyGen

	if serverLastSeq > 0 && !matches {
		// Existing live-only history cannot be safely reordered with the current
		// cloud contract. Preserve it instead of appending older messages after it.
		return serverLastSeq, nil
	}
	if matches && record.Complete && serverLastSeq >= record.EventCount {
		return serverLastSeq, nil
	}
	if len(events) == 0 {
		ledger.Sessions[sessionID] = historySyncRecord{
			ProjectionVersion: historyProjectionVersion,
			ProjectionHash:    hash,
			EventCount:        0,
			NextSeq:           1,
			KeyGen:            keyGen,
			Complete:          true,
		}
		return serverLastSeq, saveHistorySyncLedger(path, ledger)
	}

	if !matches || serverLastSeq == 0 {
		record = historySyncRecord{
			ProjectionVersion: historyProjectionVersion,
			ProjectionHash:    hash,
			EventCount:        int64(len(events)),
			NextSeq:           1,
			KeyGen:            keyGen,
		}
		ledger.Sessions[sessionID] = record
		if err := saveHistorySyncLedger(path, ledger); err != nil {
			return serverLastSeq, err
		}
	}

	startSeq := serverLastSeq + 1
	if record.NextSeq > startSeq {
		startSeq = record.NextSeq
	}
	if startSeq > int64(len(events)) {
		record.Complete = true
		record.NextSeq = int64(len(events)) + 1
		ledger.Sessions[sessionID] = record
		return maxInt64(serverLastSeq, int64(len(events))), saveHistorySyncLedger(path, ledger)
	}

	for start := startSeq; start <= int64(len(events)); {
		end := start + historyUploadBatchSize - 1
		if end > int64(len(events)) {
			end = int64(len(events))
		}
		batch := make([]EventUpload, 0, end-start+1)
		for seq := start; seq <= end; seq++ {
			event := events[seq-1]
			payload, sealedKeyGen, sealErr := c.strictSealHistory(event.Payload)
			if sealErr != nil {
				return serverLastSeq, sealErr
			}
			if sealedKeyGen != record.KeyGen {
				return serverLastSeq, fmt.Errorf("cloud history encryption key changed during backfill")
			}
			batch = append(batch, EventUpload{Seq: seq, Kind: event.Kind, Payload: payload})
		}
		resp, uploadErr := c.client.UploadEvents(ctx, c.token, sessionID, batch)
		if uploadErr != nil {
			return serverLastSeq, uploadErr
		}
		// This writer starts strictly after the upsert's server high-water mark,
		// so a conflict means another connector allocated the same seq while the
		// backfill was in flight. Do not mark an unknown payload as our history.
		if len(resp.Conflicted) > 0 {
			return serverLastSeq, fmt.Errorf("cloud history upload conflicted at seq %v; another writer is active", resp.Conflicted)
		}
		if resp.MaxSeq < end || len(resp.Accepted) != len(batch) {
			return serverLastSeq, fmt.Errorf("cloud history upload acknowledged an incomplete batch through seq %d", end)
		}
		record.NextSeq = end + 1
		ledger.Sessions[sessionID] = record
		if err := saveHistorySyncLedger(path, ledger); err != nil {
			return serverLastSeq, err
		}
		serverLastSeq = maxInt64(serverLastSeq, resp.MaxSeq)
		start = end + 1
	}
	record.Complete = true
	ledger.Sessions[sessionID] = record
	if err := saveHistorySyncLedger(path, ledger); err != nil {
		return serverLastSeq, err
	}
	return maxInt64(serverLastSeq, int64(len(events))), nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
