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
	"path/filepath"
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

	// InboxDir is the root under which chat attachments land
	// (<InboxDir>/<session_id>/<filename>). Empty → ~/.jcode/inbox.
	InboxDir string
	// ModelCapabilitiesFn overrides the model/effort facet of the capabilities
	// mirror (default: config + model registry); tests inject a fixed list.
	ModelCapabilitiesFn func() ([]CapabilityModel, []string, error)
	// SlashCommandsFn overrides the slash-commands facet of the capabilities
	// mirror (default: GET /api/slash-commands on the local control plane);
	// tests inject a fixed list.
	SlashCommandsFn func() ([]CapabilitySlashCommand, error)

	// Cipher, when non-nil, seals uplink payloads (events/ephemeral payload,
	// sessions meta, ack result) and opens downlink command payloads. Nil
	// means plaintext uplink (pre-CEK grey period; see crypto.go). Run lazily
	// initializes it from ~/.jcode/cloud.json when left nil; tests inject one
	// explicitly (or set CipherDisabled to force the plaintext path).
	Cipher         *EnvelopeCipher
	CipherDisabled bool
}

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultPollWait          = 25 * time.Second
	defaultIndexPollInterval = 2 * time.Second
	defaultBatchWindow       = 200 * time.Millisecond
	defaultBatchMax          = 20
)

// Connector states surfaced via Status (supervisor → GET /api/cloud/status).
const (
	StateOffline    = "offline"
	StateConnecting = "connecting"
	StateOnline     = "online"
	StateError      = "error"
)

// Connector is the cloud relay client. Construct with NewConnector, then Run.
type Connector struct {
	cfg    ConnectorConfig
	client *Client
	local  *localControlPlane
	seq    *seqAllocator
	text   *agentTextBuffers
	token  string
	cipher *EnvelopeCipher // nil = plaintext uplink (grey period)

	// statusMu guards state/lastError, the live connection snapshot exposed
	// via Status. Written by Run's loops, read by the web status endpoint.
	statusMu sync.Mutex
	state    string
	lastErr  string

	// pairMu guards the pairing inbox (pending approvals + the last-paired
	// notification), written by pairing.request commands and read/mutated by
	// the web pairing endpoints (via the Supervisor).
	pairMu     sync.Mutex
	pending    []PendingPairing
	lastPaired *PairedInfo
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
		text:   newAgentTextBuffers(),
		token:  token,
		cipher: cfg.Cipher,
		state:  StateOffline,
	}
}

// setState publishes a connection-state transition (see the State* constants).
func (c *Connector) setState(state, lastErr string) {
	c.statusMu.Lock()
	c.state = state
	c.lastErr = lastErr
	c.statusMu.Unlock()
}

// clearError returns the state to online after a failed loop recovers. It is
// a no-op unless the current state is StateError, so it never masks a fresh
// transition made by another loop.
func (c *Connector) clearError() {
	c.statusMu.Lock()
	if c.state == StateError {
		c.state = StateOnline
		c.lastErr = ""
	}
	c.statusMu.Unlock()
}

// Status returns the current connection state and the last error message
// (empty when none). Safe to call from any goroutine, including before Run.
func (c *Connector) Status() (state string, lastErr string) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if c.state == "" {
		return StateOffline, ""
	}
	return c.state, c.lastErr
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

// sealUplink encrypts one uplink payload field when the CEK cipher is active;
// on the plaintext grey path (or on seal failure, which must never block the
// relay) it returns the input unchanged.
func (c *Connector) sealUplink(plaintext json.RawMessage) json.RawMessage {
	if c.cipher == nil || len(plaintext) == 0 {
		return plaintext
	}
	sealed, err := c.cipher.Seal(plaintext)
	if err != nil {
		c.logf("seal failed, sending plaintext: %v", err)
		return plaintext
	}
	return sealed
}

// openDownlink decrypts a downlink command payload when it is an envelope;
// plaintext payloads pass through unchanged (grey rule). An envelope that
// fails to decrypt is a hard error — the command must not run.
func (c *Connector) openDownlink(payload json.RawMessage) (json.RawMessage, error) {
	if len(payload) == 0 {
		return payload, nil
	}
	if c.cipher == nil {
		if IsEnvelope(payload) {
			return nil, fmt.Errorf("received encrypted command but no CEK is initialized on this device")
		}
		return payload, nil
	}
	plain, _, err := c.cipher.OpenMaybe(payload)
	if err != nil {
		return nil, fmt.Errorf("decrypt downlink payload: %w", err)
	}
	return plain, nil
}

// Run starts the connector and blocks until ctx is cancelled (web server
// shutdown). It never returns an error — all failures are logged as warnings.
func (c *Connector) Run(ctx context.Context) {
	if c.cfg.Credentials == nil || c.token == "" {
		c.logf("connector not started: no device credentials")
		return
	}
	// Stopping (ctx cancel) always lands back on offline, whatever the loops
	// were doing.
	defer c.setState(StateOffline, "")
	// E2E (M5): lazily initialize the CEK cipher. Before the CEK exists the
	// connector stays on the plaintext grey path; once generated, everything
	// uplink is sealed. A failure here must never stop the connector — fall
	// back to plaintext with a warning.
	if c.cipher == nil && !c.cfg.CipherDisabled {
		if cipher, err := EnsureCEK(); err != nil {
			c.logf("CEK initialization failed, staying on plaintext uplink: %v", err)
		} else {
			c.cipher = cipher
			c.logf("E2E encryption active (key_gen=%d)", cipher.KeyGen())
		}
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
		c.setState(StateConnecting, "")
		err := c.client.RegisterDevice(ctx, c.token, RegisterDeviceRequest{
			Name:         name,
			Hostname:     hostname,
			JcodeVersion: c.cfg.Version,
			PubKey:       c.cfg.Credentials.PublicKey,
			Platform:     detectPlatform(),
			E2EE:         c.e2eeActive(),
		})
		if err == nil {
			c.setState(StateOnline, "")
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.setState(StateError, err.Error())
		c.logf("device register failed: %v", err)
		if werr := bo.Wait(ctx); werr != nil {
			return werr
		}
	}
}

// e2eeActive reports the ACTUAL uplink encryption state sent as `e2ee` at
// register (M13): true only when the CEK cipher initialized AND cloud.e2ee did
// not disable it (CipherDisabled). Run initializes the cipher before the
// register loop, so this reflects the live grey/encrypted path, not the raw
// config flag.
func (c *Connector) e2eeActive() bool {
	return c.cipher != nil && !c.cfg.CipherDisabled
}

// detectPlatform reports how this jcode instance was launched, sent as the// `platform` field at device register. The desktop app spawns `jcode web` as
// a Tauri sidecar with JCODE_DESKTOP=1 in its environment (set in
// desktop/src-tauri/src/sidecar.rs); every other launch is the CLI.
func detectPlatform() string {
	if os.Getenv("JCODE_DESKTOP") == "1" {
		return "desktop"
	}
	return "cli"
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
			err := c.client.Heartbeat(ctx, c.token)
			if err != nil && ctx.Err() == nil {
				c.setState(StateError, err.Error())
				c.logf("heartbeat failed: %v", err)
			}
			if err == nil {
				c.clearError() // heartbeat recovery: back to online
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
			c.setState(StateError, err.Error())
			c.logf("poll failed: %v", err)
			if werr := bo.Wait(ctx); werr != nil {
				return
			}
			continue
		}
		bo.Reset()
		c.clearError() // poll recovery: back to online
		if !ok {
			continue // 204: no commands, poll again right away
		}
		for _, cmd := range cmds {
			c.executeAndAck(ctx, cmd)
		}
	}
}

// executeAndAck runs one downlink command against the local control plane and
// posts the ack. The ack itself is best-effort (warned on failure). Downlink
// payloads are decrypted (envelope) before dispatch; the ack result is sealed
// when the CEK cipher is active.
func (c *Connector) executeAndAck(ctx context.Context, cmd DeviceCommand) {
	payload, err := c.openDownlink(cmd.Payload)
	if err != nil {
		c.logf("command %s (%s) rejected: %v", cmd.ID, cmd.Kind, err)
		c.ack(ctx, cmd.ID, "error", map[string]string{"error": err.Error()})
		return
	}
	cmd.Payload = payload
	status, result := c.executeCommand(ctx, cmd)
	if status == "error" {
		c.logf("command %s (%s) failed: %v", cmd.ID, cmd.Kind, result)
	} else {
		c.logf("command %s (%s) executed", cmd.ID, cmd.Kind)
	}
	c.ack(ctx, cmd.ID, status, result)
}

// ack posts one command ack, sealing the result when encryption is active.
func (c *Connector) ack(ctx context.Context, id, status string, result any) {
	if c.cipher != nil && result != nil {
		if plain, err := json.Marshal(result); err == nil {
			result = c.sealUplink(plain)
		}
	}
	if err := c.client.AckCommand(ctx, c.token, id, CommandAck{Status: status, Result: result}); err != nil && ctx.Err() == nil {
		c.logf("ack for command %s failed: %v", id, err)
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
	return l.doJSON(ctx, http.MethodPost, path, bytes.NewReader(data))
}

// getJSON GETs the local control plane and returns the status and raw body.
func (l *localControlPlane) getJSON(ctx context.Context, path string) (int, []byte, error) {
	return l.doJSON(ctx, http.MethodGet, path, nil)
}

func (l *localControlPlane) doJSON(ctx context.Context, method, path string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, l.base+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if l.token != "" {
		req.Header.Set("Authorization", "Bearer "+l.token)
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("%s %s: read response: %w", method, path, err)
	}
	return resp.StatusCode, respBody, nil
}

// chatSendPayload is the (plaintext) payload of a chat.send command. Beyond
// the original text/images/mode/channel it carries the M12 compose facets —
// project_path, model, effort, goal, attachments — all optional. goal_armed
// (M14) flips the meaning of text: it is a goal objective and the command
// only arms the goal (POST /api/goal with start=true), skipping /api/chat and
// every other compose step.
type chatSendPayload struct {
	Text        string           `json:"text"`
	Images      []chatImage      `json:"images,omitempty"`
	Mode        string           `json:"mode,omitempty"`
	Channel     string           `json:"channel,omitempty"` // "console" | "mobile"
	ProjectPath string           `json:"project_path,omitempty"`
	Model       *chatModelRef    `json:"model,omitempty"`
	Effort      string           `json:"effort,omitempty"`
	Goal        string           `json:"goal,omitempty"`
	GoalArmed   bool             `json:"goal_armed,omitempty"`
	Attachments []chatAttachment `json:"attachments,omitempty"`
}

// chatModelRef is the model facet of a compose chat.send.
type chatModelRef struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

// needsCompose reports whether the payload carries any M12 compose facet and
// therefore goes through the ordered compose pipeline instead of the plain
// one-shot /api/chat call.
func (p *chatSendPayload) needsCompose() bool {
	return p.ProjectPath != "" || p.Model != nil || p.Effort != "" || p.Goal != "" || len(p.Attachments) > 0
}

// chatImage mirrors web.chatImage (base64 image in a chat request); name is
// the optional original filename (file picker / paste), passed through
// losslessly — the local /api/chat handler ignores it.
type chatImage struct {
	Data     string `json:"data"`
	MimeType string `json:"media_type"`
	Name     string `json:"name,omitempty"`
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
	case "pairing.request":
		return c.execPairingRequest(ctx, cmd)
	default:
		return "error", map[string]string{"error": fmt.Sprintf("unknown command kind %q", cmd.Kind)}
	}
}

func (c *Connector) execChatSend(ctx context.Context, cmd DeviceCommand) (string, any) {
	var p chatSendPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return "error", map[string]string{"error": fmt.Sprintf("invalid chat.send payload: %v", err)}
	}
	// goal_armed wins over everything: text is the goal objective and the
	// command only arms the goal — /api/chat and all compose facets
	// (mode/images/session/attachments/…) are ignored.
	if p.GoalArmed {
		return c.execChatSendGoalArmed(ctx, &p)
	}
	// Attachments alone are a valid message (their reference list becomes the
	// text); truly empty input is not.
	if strings.TrimSpace(p.Text) == "" && len(p.Attachments) == 0 {
		return "error", map[string]string{"error": "chat.send: empty text"}
	}
	if p.needsCompose() {
		return c.execChatSendCompose(ctx, cmd, &p)
	}
	return c.execChatSendLegacy(ctx, cmd, &p)
}

// execChatSendGoalArmed arms the session goal on the active engine via
// POST /api/goal with start=true (mirroring the web UI's setGoal default),
// which kicks off the agent run itself — no /api/chat call follows.
func (c *Connector) execChatSendGoalArmed(ctx context.Context, p *chatSendPayload) (string, any) {
	objective := strings.TrimSpace(p.Text)
	if objective == "" {
		return "error", map[string]string{"error": "chat.send: goal_armed with empty objective"}
	}
	status, body, err := c.local.postJSON(ctx, "/api/goal", map[string]any{
		"objective": objective, "start": true,
	})
	if err != nil {
		return "error", map[string]string{"error": err.Error()}
	}
	if status != http.StatusOK {
		return "error", map[string]string{"error": errUnexpectedStatus("/api/goal", status, string(body)).Error()}
	}
	return "ok", json.RawMessage(body)
}

// execChatSendLegacy is the pre-M12 one-shot path: a single /api/chat call,
// optionally continuing cmd.SessionID.
func (c *Connector) execChatSendLegacy(ctx context.Context, cmd DeviceCommand, p *chatSendPayload) (string, any) {
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

// execChatSendCompose runs the M12 ordered compose pipeline against the local
// control plane: create/focus the session (project_path) → land attachments →
// model → effort → mode → goal → send the message with the attachment
// reference list appended. Every step failure acks error naming the facet —
// unsupported facets are never silently ignored.
func (c *Connector) execChatSendCompose(ctx context.Context, cmd DeviceCommand, p *chatSendPayload) (string, any) {
	errResult := func(err error) (string, any) {
		return "error", map[string]string{"error": err.Error()}
	}

	// 0. Validate and decode attachments BEFORE any side effect, so a limit
	// breach leaves no trace.
	decoded, err := decodeAttachments(p.Attachments)
	if err != nil {
		return errResult(err)
	}

	// 1. Create/focus the session. POST /api/sessions makes the task the
	// active engine — the model/mode/goal endpoints all target the active
	// engine — and returns its id, which the attachments dir and the chat
	// call both need.
	sessReq := map[string]string{}
	if cmd.SessionID != "" {
		sessReq["session_id"] = cmd.SessionID
	}
	if p.ProjectPath != "" {
		sessReq["pwd"] = p.ProjectPath
	}
	status, body, err := c.local.postJSON(ctx, "/api/sessions", sessReq)
	if err != nil {
		return errResult(err)
	}
	if status != http.StatusOK {
		return errResult(errUnexpectedStatus("/api/sessions", status, string(body)))
	}
	var sessResp struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(body, &sessResp); err != nil || sessResp.SessionID == "" {
		return errResult(fmt.Errorf("/api/sessions: no session_id in response: %s", body))
	}
	sid := sessResp.SessionID

	// 2. Land attachments at <inbox>/<sid>/.
	var refs []attachmentRef
	if len(p.Attachments) > 0 {
		refs, err = writeInboxAttachments(c.inboxRoot(), sid, p.Attachments, decoded)
		if err != nil {
			return errResult(err)
		}
	}

	// 3. Model, then effort (the effort endpoint is keyed by provider+model).
	if p.Model != nil {
		if p.Model.Provider == "" || p.Model.ID == "" {
			return errResult(fmt.Errorf("model: provider and id are both required"))
		}
		if err := c.postLocalOK(ctx, "/api/model", map[string]string{
			"provider": p.Model.Provider, "model": p.Model.ID,
		}); err != nil {
			return errResult(fmt.Errorf("model: %w", err))
		}
	}
	if p.Effort != "" {
		provider, modelID := "", ""
		if p.Model != nil {
			provider, modelID = p.Model.Provider, p.Model.ID
		} else {
			provider, modelID, err = c.currentModel(ctx)
			if err != nil {
				return errResult(fmt.Errorf("effort: cannot resolve current model: %w", err))
			}
		}
		if provider == "" || modelID == "" {
			return errResult(fmt.Errorf("effort: no current model to apply effort %q to", p.Effort))
		}
		if err := c.postLocalOK(ctx, "/api/model-state/effort", map[string]string{
			"provider": provider, "model": modelID, "effort": p.Effort,
		}); err != nil {
			return errResult(fmt.Errorf("effort: %w", err))
		}
	}
	// Mode: the chat endpoint's mode field only applies to engines IT builds;
	// the compose session already exists, so switch it on the focused engine.
	if p.Mode != "" {
		m := p.Mode
		if m == "build" { // legacy chat-mode alias for the approval mode
			m = "approval"
		}
		if err := c.postLocalOK(ctx, "/api/mode", map[string]string{"mode": m}); err != nil {
			return errResult(fmt.Errorf("mode: %w", err))
		}
	}

	// 4. Goal (start=false: the message below kicks the run off itself).
	if p.Goal != "" {
		if err := c.postLocalOK(ctx, "/api/goal", map[string]any{
			"objective": p.Goal, "start": false,
		}); err != nil {
			return errResult(fmt.Errorf("goal: %w", err))
		}
	}

	// 5. Send the message with the attachment reference list appended.
	req := map[string]any{
		"message":    p.Text + attachmentReferenceList(refs),
		"session_id": sid,
	}
	if len(p.Images) > 0 {
		req["images"] = p.Images
	}
	if p.Channel != "" {
		req["source"] = p.Channel
	}
	status, body, err = c.local.postJSON(ctx, "/api/chat", req)
	if err != nil {
		return errResult(err)
	}
	if status != http.StatusAccepted && status != http.StatusOK {
		return errResult(errUnexpectedStatus("/api/chat", status, string(body)))
	}
	return "ok", map[string]string{"session_id": sid}
}

// postLocalOK POSTs to the local control plane expecting a 200 OK.
func (c *Connector) postLocalOK(ctx context.Context, path string, body any) error {
	status, respBody, err := c.local.postJSON(ctx, path, body)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return errUnexpectedStatus(path, status, string(respBody))
	}
	return nil
}

// currentModel resolves the active engine's provider/model via GET
// /api/health (used to key an effort override when the command did not name
// a model).
func (c *Connector) currentModel(ctx context.Context) (provider, modelID string, err error) {
	status, body, err := c.local.getJSON(ctx, "/api/health")
	if err != nil {
		return "", "", err
	}
	if status != http.StatusOK {
		return "", "", errUnexpectedStatus("/api/health", status, string(body))
	}
	var health struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		return "", "", fmt.Errorf("/api/health: invalid response: %w", err)
	}
	return health.Provider, health.Model, nil
}

// inboxRoot returns the attachment inbox root (default ~/.jcode/inbox).
func (c *Connector) inboxRoot() string {
	if c.cfg.InboxDir != "" {
		return c.cfg.InboxDir
	}
	return filepath.Join(config.ConfigDir(), "inbox")
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
