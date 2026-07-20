// connector.go implements the jcloud relay connector: the local jcode poses
// as an always-on runner toward the cloud, over purely outbound connections
// (long poll + POSTs). It forwards downlink commands to the local web control
// plane (127.0.0.1) and pumps local WS events uplink. Every failure is logged
// as a warning — the connector must never affect the local web server.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
)

// ConnectorConfig carries everything the connector needs. The zero durations
// fall back to production defaults; tests inject short ones.
type ConnectorConfig struct {
	CloudURL    string       // orchestrator base URL (config.cloud.url, else credentials)
	Credentials *Credentials // device identity from ~/.jcode/cloud.json
	LocalBase   string       // local web control plane, e.g. http://127.0.0.1:8080
	LocalToken  string       // web auth token (empty when the server doesn't require auth)
	Version     string       // jcode version reported at register

	// Optional knobs (tests inject these; zero values use defaults).
	HTTPClient        *http.Client
	Hostname          string
	HeartbeatInterval time.Duration
	PollWait          time.Duration
	IndexPollInterval time.Duration
	BatchWindow       time.Duration
	BatchMax          int
	Backoff           *Backoff
	// IndexPathFn / ListSessionsFn default to the real session index; tests
	// override them to point at a temp dir.
	IndexPathFn    func() (string, error)
	ListSessionsFn func() (map[string][]session.SessionMeta, error)
}

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultPollWait          = 25 * time.Second
	defaultIndexPollInterval = 2 * time.Second
	defaultBatchWindow       = 200 * time.Millisecond
	defaultBatchMax          = 20
)

// Connector is the cloud relay client. Construct with NewConnector, then Run.
type Connector struct {
	cfg    ConnectorConfig
	client *Client
	local  *localControlPlane
	seq    *seqAllocator
	token  string
}

// NewConnector builds a Connector from cfg.
func NewConnector(cfg ConnectorConfig) *Connector {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	client := NewClient(cfg.CloudURL)
	client.HTTPClient = hc
	token := ""
	if cfg.Credentials != nil {
		token = cfg.Credentials.DeviceToken
	}
	return &Connector{
		cfg:    cfg,
		client: client,
		local:  &localControlPlane{base: strings.TrimRight(cfg.LocalBase, "/"), token: cfg.LocalToken, http: hc},
		seq:    newSeqAllocator(),
		token:  token,
	}
}

func (c *Connector) heartbeatInterval() time.Duration {
	if c.cfg.HeartbeatInterval > 0 {
		return c.cfg.HeartbeatInterval
	}
	return defaultHeartbeatInterval
}

func (c *Connector) pollWait() time.Duration {
	if c.cfg.PollWait > 0 {
		return c.cfg.PollWait
	}
	return defaultPollWait
}

func (c *Connector) indexPollInterval() time.Duration {
	if c.cfg.IndexPollInterval > 0 {
		return c.cfg.IndexPollInterval
	}
	return defaultIndexPollInterval
}

func (c *Connector) batchWindow() time.Duration {
	if c.cfg.BatchWindow > 0 {
		return c.cfg.BatchWindow
	}
	return defaultBatchWindow
}

func (c *Connector) batchMax() int {
	if c.cfg.BatchMax > 0 {
		return c.cfg.BatchMax
	}
	return defaultBatchMax
}

func (c *Connector) backoff() *Backoff {
	if c.cfg.Backoff != nil {
		return c.cfg.Backoff
	}
	return NewBackoff(1*time.Second, 60*time.Second)
}

func (c *Connector) logf(format string, args ...any) {
	config.Logger().Printf("[cloud] "+format, args...)
}

// Run starts the connector and blocks until ctx is cancelled (web server
// shutdown). It never returns an error — all failures are logged as warnings.
func (c *Connector) Run(ctx context.Context) {
	if c.cfg.Credentials == nil || c.token == "" {
		c.logf("connector not started: no device credentials")
		return
	}
	// The long poll holds a request open for pollWait; make sure the HTTP
	// client's own timeout gives the server room to answer.
	if c.client.HTTPClient != nil && c.client.HTTPClient.Timeout > 0 &&
		c.client.HTTPClient.Timeout < c.pollWait()+10*time.Second {
		pollClient := *c.client.HTTPClient
		pollClient.Timeout = c.pollWait() + 10*time.Second
		c.client.HTTPClient = &pollClient
	}

	// Register first, retrying with backoff: the orchestrator may briefly be
	// unreachable while the local web server is already up.
	if err := c.registerLoop(ctx); err != nil {
		return // ctx cancelled while registering
	}
	c.logf("connected to %s as device %s", c.cfg.CloudURL, c.cfg.Credentials.DeviceID)

	// Seed seq numbers from the server's per-session high-water marks BEFORE
	// the event pump starts assigning seqs (续号). Best-effort: a failure just
	// means the next successful upsert seeds instead.
	if err := c.syncSessions(ctx); err != nil {
		c.logf("initial session upsert failed (will retry): %v", err)
	}

	var wg sync.WaitGroup
	for name, fn := range map[string]func(context.Context){
		"heartbeat":    c.heartbeatLoop,
		"poll":         c.pollLoop,
		"event_pump":   c.eventPumpLoop,
		"session_sync": c.sessionSyncLoop,
	} {
		wg.Add(1)
		go func(name string, fn func(context.Context)) {
			defer wg.Done()
			fn(ctx)
			c.logf("%s stopped", name)
		}(name, fn)
	}
	wg.Wait()
	c.logf("disconnected")
}

// registerLoop attempts device registration until it succeeds or ctx ends.
func (c *Connector) registerLoop(ctx context.Context) error {
	bo := c.backoff()
	hostname := c.cfg.Hostname
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	name := c.cfg.Credentials.DeviceName
	if name == "" {
		name = hostname
	}
	for {
		err := c.client.RegisterDevice(ctx, c.token, RegisterDeviceRequest{
			Name:         name,
			Hostname:     hostname,
			JcodeVersion: c.cfg.Version,
			PubKey:       c.cfg.Credentials.PublicKey,
		})
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.logf("device register failed: %v", err)
		if werr := bo.Wait(ctx); werr != nil {
			return werr
		}
	}
}

// heartbeatLoop POSTs a heartbeat every heartbeatInterval.
func (c *Connector) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.heartbeatInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.client.Heartbeat(ctx, c.token); err != nil && ctx.Err() == nil {
				c.logf("heartbeat failed: %v", err)
			}
		}
	}
}

// pollLoop long-polls the downlink command queue. A 204 polls again
// immediately; network/HTTP errors back off exponentially.
func (c *Connector) pollLoop(ctx context.Context) {
	bo := c.backoff()
	for {
		cmds, ok, err := c.client.PollCommands(ctx, c.token, c.pollWait())
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logf("poll failed: %v", err)
			if werr := bo.Wait(ctx); werr != nil {
				return
			}
			continue
		}
		bo.Reset()
		if !ok {
			continue // 204: no commands, poll again right away
		}
		for _, cmd := range cmds {
			c.executeAndAck(ctx, cmd)
		}
	}
}

// executeAndAck runs one downlink command against the local control plane and
// posts the ack. The ack itself is best-effort (warned on failure).
func (c *Connector) executeAndAck(ctx context.Context, cmd DeviceCommand) {
	status, result := c.executeCommand(ctx, cmd)
	if status == "error" {
		c.logf("command %s (%s) failed: %v", cmd.ID, cmd.Kind, result)
	} else {
		c.logf("command %s (%s) executed", cmd.ID, cmd.Kind)
	}
	if err := c.client.AckCommand(ctx, c.token, cmd.ID, CommandAck{Status: status, Result: result}); err != nil && ctx.Err() == nil {
		c.logf("ack for command %s failed: %v", cmd.ID, err)
	}
}

// --- command execution against the local web control plane ---

// localControlPlane is a minimal client for the local jcode web REST API.
type localControlPlane struct {
	base  string
	token string
	http  *http.Client
}

// postJSON POSTs body to the local control plane and returns the status and
// raw response body.
func (l *localControlPlane) postJSON(ctx context.Context, path string, body any) (int, []byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal local request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.base+path, bytes.NewReader(data))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if l.token != "" {
		req.Header.Set("Authorization", "Bearer "+l.token)
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("POST %s: read response: %w", path, err)
	}
	return resp.StatusCode, respBody, nil
}

// chatSendPayload is the (plaintext) payload of a chat.send command.
type chatSendPayload struct {
	Text    string      `json:"text"`
	Images  []chatImage `json:"images,omitempty"`
	Mode    string      `json:"mode,omitempty"`
	Channel string      `json:"channel,omitempty"` // "console" | "mobile"
}

// chatImage mirrors web.chatImage (base64 image in a chat request).
type chatImage struct {
	Data     string `json:"data"`
	MimeType string `json:"media_type"`
}

// approvalRespondPayload is the payload of an approval.respond command.
type approvalRespondPayload struct {
	ApprovalID string `json:"approval_id"`
	Decision   string `json:"decision"` // "approve" | "approve_all" | "deny"
}

// executeCommand dispatches one command to the local control plane and
// returns the ack status ("ok"|"error") plus the ack result payload.
func (c *Connector) executeCommand(ctx context.Context, cmd DeviceCommand) (string, any) {
	switch cmd.Kind {
	case "chat.send":
		return c.execChatSend(ctx, cmd)
	case "chat.stop":
		return c.execChatStop(ctx, cmd)
	case "approval.respond":
		return c.execApprovalRespond(ctx, cmd)
	default:
		return "error", map[string]string{"error": fmt.Sprintf("unknown command kind %q", cmd.Kind)}
	}
}

func (c *Connector) execChatSend(ctx context.Context, cmd DeviceCommand) (string, any) {
	var p chatSendPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return "error", map[string]string{"error": fmt.Sprintf("invalid chat.send payload: %v", err)}
	}
	if strings.TrimSpace(p.Text) == "" {
		return "error", map[string]string{"error": "chat.send: empty text"}
	}
	// The local /api/chat request. session_id is omitted for a NEW session
	// (the server mints the UUID); source carries the relay channel through
	// the same mechanism WeChat uses (submitMessage's source label).
	req := map[string]any{
		"message": p.Text,
	}
	if len(p.Images) > 0 {
		req["images"] = p.Images
	}
	if p.Mode != "" {
		req["mode"] = p.Mode
	}
	if cmd.SessionID != "" {
		req["session_id"] = cmd.SessionID
	}
	if p.Channel != "" {
		req["source"] = p.Channel
	}
	status, body, err := c.local.postJSON(ctx, "/api/chat", req)
	if err != nil {
		return "error", map[string]string{"error": err.Error()}
	}
	if status != http.StatusAccepted && status != http.StatusOK {
		return "error", map[string]string{"error": errUnexpectedStatus("/api/chat", status, string(body)).Error()}
	}
	var resp struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(body, &resp)
	return "ok", map[string]string{"session_id": resp.SessionID}
}

func (c *Connector) execChatStop(ctx context.Context, cmd DeviceCommand) (string, any) {
	// The local stop endpoint keys on task_id (= session UUID).
	status, body, err := c.local.postJSON(ctx, "/api/stop", map[string]string{"task_id": cmd.SessionID})
	if err != nil {
		return "error", map[string]string{"error": err.Error()}
	}
	if status != http.StatusOK {
		return "error", map[string]string{"error": errUnexpectedStatus("/api/stop", status, string(body)).Error()}
	}
	return "ok", json.RawMessage(body)
}

func (c *Connector) execApprovalRespond(ctx context.Context, cmd DeviceCommand) (string, any) {
	var p approvalRespondPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return "error", map[string]string{"error": fmt.Sprintf("invalid approval.respond payload: %v", err)}
	}
	if p.ApprovalID == "" {
		return "error", map[string]string{"error": "approval.respond: empty approval_id"}
	}
	var approved, approveAll bool
	switch p.Decision {
	case "approve":
		approved = true
	case "approve_all":
		approved, approveAll = true, true
	case "deny", "reject":
		approved = false
	default:
		return "error", map[string]string{"error": fmt.Sprintf("approval.respond: unknown decision %q", p.Decision)}
	}
	status, body, err := c.local.postJSON(ctx, "/api/approval", map[string]any{
		"id":          p.ApprovalID,
		"task_id":     cmd.SessionID,
		"approved":    approved,
		"approve_all": approveAll,
	})
	if err != nil {
		return "error", map[string]string{"error": err.Error()}
	}
	if status != http.StatusOK {
		return "error", map[string]string{"error": errUnexpectedStatus("/api/approval", status, string(body)).Error()}
	}
	return "ok", json.RawMessage(body)
}
