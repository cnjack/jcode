// relay.go implements the jcode ↔ orchestrator device-relay protocol
// (cloud/docs/17-jcode-device-relay.md §4): long-poll command delivery,
// command acks, heartbeats, session-index upserts and event uploads.
//
// Payload fields are opaque to the server: since M5 they are E2E envelopes
// (see crypto.go) whenever the device's CEK cipher is active, plaintext
// during the pre-CEK grey period. Sealing/opening happens in the Connector
// (sealUplink / openDownlink), not in these transport helpers.
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DeviceCommand is one downlink command delivered by the poll endpoint.
type DeviceCommand struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"` // chat.send | chat.stop | approval.respond | …
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// CommandAck is the uplink execution receipt for one command.
type CommandAck struct {
	Status string `json:"status"` // "ok" | "error"
	Result any    `json:"result,omitempty"`
}

// SessionUpsert mirrors one local session's index entry to the cloud.
type SessionUpsert struct {
	SessionID      string          `json:"session_id"`
	Status         string          `json:"status"`                     // "running" | "idle"
	Meta           json.RawMessage `json:"meta"`                       // SessionMeta JSON, as-is
	LastActivityAt string          `json:"last_activity_at,omitempty"` // UTC RFC3339, intentionally plaintext for cloud-side ordering
}

// SessionSeqInfo is the server's per-session high-water mark, returned by the
// sessions upsert so the event pump can resume seq numbering after a restart.
type SessionSeqInfo struct {
	SessionID string `json:"session_id"`
	LastSeq   int64  `json:"last_seq"`
}

// SessionsUpsertResponse is the answer of POST /internal/v1/device/sessions.
type SessionsUpsertResponse struct {
	Sessions []SessionSeqInfo `json:"sessions"`
}

// EventUpload is one durable event in a batch upload.
type EventUpload struct {
	Seq     int64           `json:"seq"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// EventsUploadResponse is the answer of POST
// /internal/v1/device/sessions/{sid}/events. Conflicted lists seqs the server
// already had (skipped); MaxSeq is the server's high-water mark for the
// session and is used to resync the local allocator.
type EventsUploadResponse struct {
	Accepted   []int64 `json:"accepted"`
	Conflicted []int64 `json:"conflicted"`
	MaxSeq     int64   `json:"max_seq"`
}

// Heartbeat keeps the device marked online: POST
// /internal/v1/device/heartbeat (empty body).
func (c *Client) Heartbeat(ctx context.Context, token string) error {
	return c.post(ctx, "/internal/v1/device/heartbeat", token, map[string]string{}, nil)
}

// PollCommands long-polls the downlink queue: GET
// /internal/v1/device/poll?wait=<wait>. ok is false on a 204 (no commands),
// in which case the caller should poll again immediately.
func (c *Client) PollCommands(ctx context.Context, token string, wait time.Duration) (cmds []DeviceCommand, ok bool, err error) {
	path := "/internal/v1/device/poll?wait=" + url.QueryEscape(wait.String())
	var out struct {
		Commands []DeviceCommand `json:"commands"`
	}
	status, err := c.get(ctx, path, token, &out)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusNoContent {
		return nil, false, nil
	}
	return out.Commands, true, nil
}

// AckCommand reports a command's execution result: POST
// /internal/v1/device/commands/{id}/ack.
func (c *Client) AckCommand(ctx context.Context, token, id string, ack CommandAck) error {
	return c.post(ctx, "/internal/v1/device/commands/"+url.PathEscape(id)+"/ack", token, ack, nil)
}

// UpsertSessions mirrors the local session index to the cloud: POST
// /internal/v1/device/sessions. The response carries each session's last_seq
// for event-pump seq resumption. capabilities is the M12 device-capabilities
// mirror (DeviceCapabilities JSON, sealed when the CEK cipher is active),
// stored by the orchestrator in devices.capabilities; nil omits the field.
func (c *Client) UpsertSessions(ctx context.Context, token string, sessions []SessionUpsert, capabilities json.RawMessage) (*SessionsUpsertResponse, error) {
	var out SessionsUpsertResponse
	body := struct {
		Sessions     []SessionUpsert `json:"sessions"`
		Capabilities json.RawMessage `json:"capabilities,omitempty"`
		Replace      bool            `json:"replace"`
	}{Sessions: sessions, Capabilities: capabilities, Replace: true}
	if err := c.post(ctx, "/internal/v1/device/sessions", token, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadEvents appends a batch of durable events: POST
// /internal/v1/device/sessions/{sid}/events. Idempotent — the server skips
// (sid, seq) pairs it already holds and reports them as conflicted.
func (c *Client) UploadEvents(ctx context.Context, token, sid string, events []EventUpload) (*EventsUploadResponse, error) {
	var out EventsUploadResponse
	body := struct {
		Events []EventUpload `json:"events"`
	}{Events: events}
	if err := c.post(ctx, "/internal/v1/device/sessions/"+url.PathEscape(sid)+"/events", token, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SendEphemeral forwards one ephemeral (token-level) event: POST
// /internal/v1/device/sessions/{sid}/ephemeral. Fire-and-forget — failures are
// dropped by the caller, never retried.
func (c *Client) SendEphemeral(ctx context.Context, token, sid, kind string, payload json.RawMessage) error {
	body := struct {
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}{Kind: kind, Payload: payload}
	return c.post(ctx, "/internal/v1/device/sessions/"+url.PathEscape(sid)+"/ephemeral", token, body, nil)
}

// ShouldConnect reports whether the relay connector should start: the device
// must be logged in (creds present with a token) and auto_connect must not be
// explicitly disabled. Kept pure so the startup gate is unit-testable.
func ShouldConnect(autoConnect bool, creds *Credentials) bool {
	if !autoConnect || creds == nil {
		return false
	}
	return creds.DeviceToken != ""
}

// errUnexpectedStatus is a small helper for local-control-plane calls.
func errUnexpectedStatus(api string, status int, body string) error {
	return fmt.Errorf("%s: unexpected status %d: %s", api, status, body)
}
