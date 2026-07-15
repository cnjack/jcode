package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrControlInterrupted is returned when the user (or the extension) takes back
// control of a tab mid-action. Tools surface this so the model stops and
// reports naturally rather than retrying.
var ErrControlInterrupted = fmt.Errorf("browser control interrupted")

// Session is the per-task browser state: one backend, a set of controlled
// tabs, the active tab, and the latest snapshot generation for stale-uid
// detection. It is safe for concurrent use by the tool layer.
type Session struct {
	mu      sync.Mutex
	backend Backend
	tabs    map[string]*sessionTab
	active  string
	gen     int
	// uidSeq is the session-wide monotonic uid counter. uids are never reused,
	// so a uid absent from the latest snapshot is genuinely stale rather than
	// silently rebound to a different element. See uitree.Snapshot.
	uidSeq int
	snaps  map[string]*Snapshot // tabID → latest snapshot
}

type sessionTab struct {
	conn    TabConn
	dialog  *pendingDialog
	created bool   // created by the agent (short-lived by default)
	url     string // last known URL (refreshed on snapshot; used for origin-scoped approval)
}

type pendingDialog struct {
	Type    string
	Message string
}

// NewSession wraps a backend into a per-task session.
func NewSession(backend Backend) *Session {
	return &Session{
		backend: backend,
		tabs:    make(map[string]*sessionTab),
		snaps:   make(map[string]*Snapshot),
	}
}

// Backend returns the underlying backend kind ("managed"/"extension").
func (s *Session) BackendKind() string { return s.backend.Kind() }

// Close releases this task's tabs. It deliberately does NOT close the backend:
// the managed Chrome and the extension bridge are owned by the Manager and
// reused across tasks, so tearing the backend down here would kill the browser
// out from under every other (and future) task and leave the Manager caching a
// dead backend. Backend teardown belongs to Manager.Close.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, t := range s.tabs {
		// A managed tab the agent opened is scratch state: close it so tabs don't
		// pile up in the Chrome we reuse across tasks. Everything else — extension
		// tabs (in the user's real browser) and tabs claimed from another session
		// — is handed back via Detach rather than closed.
		if s.backend.Kind() == "managed" && t.created {
			_ = t.conn.Close(ctx)
		} else {
			_ = t.conn.Detach(ctx)
		}
	}
	s.tabs = nil
	s.active = ""
	return nil
}

// ensureActive returns the active tab, creating one if the session has none.
func (s *Session) ensureActive(ctx context.Context) (*sessionTab, error) {
	if s.active != "" {
		if t, ok := s.tabs[s.active]; ok {
			return t, nil
		}
	}
	conn, err := s.backend.NewTab(ctx, "about:blank")
	if err != nil {
		return nil, err
	}
	return s.registerTab(conn, true), nil
}

// registerTab wires event handling and enables the domains we rely on.
func (s *Session) registerTab(conn TabConn, created bool) *sessionTab {
	id := conn.ID()
	t := &sessionTab{conn: conn, created: created}
	s.tabs[id] = t
	s.active = id
	conn.SetEventHandler(func(method string, params json.RawMessage) {
		s.onEvent(id, method, params)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = conn.Send(ctx, "Page.enable", nil)
	_, _ = conn.Send(ctx, "DOM.enable", nil)
	_, _ = conn.Send(ctx, "Runtime.enable", nil)
	return t
}

func (s *Session) onEvent(tabID, method string, params json.RawMessage) {
	switch method {
	case "Page.javascriptDialogOpening":
		var d struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(params, &d); err == nil {
			s.mu.Lock()
			if t := s.tabs[tabID]; t != nil {
				t.dialog = &pendingDialog{Type: d.Type, Message: d.Message}
			}
			s.mu.Unlock()
		}
	case "Inspector.detached", "Target.detachedFromTarget":
		s.mu.Lock()
		delete(s.tabs, tabID)
		if s.active == tabID {
			s.active = ""
		}
		s.mu.Unlock()
	}
}

// --- Navigation ---

// Open navigates the active tab (or a new tab) to url and returns a fresh
// snapshot header.
func (s *Session) Open(ctx context.Context, url string, newTab bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var t *sessionTab
	if newTab {
		conn, err := s.backend.NewTab(ctx, url)
		if err != nil {
			return "", err
		}
		t = s.registerTab(conn, true)
	} else {
		var err error
		t, err = s.ensureActive(ctx)
		if err != nil {
			return "", err
		}
		if _, err := t.conn.Send(ctx, "Page.navigate", map[string]any{"url": url}); err != nil {
			return "", err
		}
	}
	s.waitForLoad(ctx, t)
	return s.snapshotLocked(ctx, t, "interactive", 40)
}

// waitForLoad gives the page a moment to settle (best-effort; snapshot is the
// real source of truth). We poll document.readyState briefly.
func (s *Session) waitForLoad(ctx context.Context, t *sessionTab) {
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		res, err := t.conn.Send(ctx, "Runtime.evaluate", map[string]any{
			"expression":    "document.readyState",
			"returnByValue": true,
		})
		if err == nil {
			var r struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			if json.Unmarshal(res, &r) == nil && (r.Result.Value == "interactive" || r.Result.Value == "complete") {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// Reload reloads the active tab.
func (s *Session) Reload(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.ensureActive(ctx)
	if err != nil {
		return "", err
	}
	if _, err := t.conn.Send(ctx, "Page.reload", nil); err != nil {
		return "", err
	}
	s.waitForLoad(ctx, t)
	return s.snapshotLocked(ctx, t, "interactive", 40)
}

// CurrentOrigin returns the scheme://host of the active tab's last known URL, or
// "" when there is no active tab or its URL has no real origin (e.g. about:blank).
// It reads cached state — refreshed on every snapshot, which the model takes
// before acting — so the approval layer can scope per-site permissions for
// actions whose args carry no URL (clicks, fills) without a blocking CDP call.
func (s *Session) CurrentOrigin() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == "" {
		return ""
	}
	t := s.tabs[s.active]
	if t == nil {
		return ""
	}
	return OriginOf(t.url)
}

// --- Snapshot ---

// Snapshot returns a uid-annotated text snapshot of the active tab.
func (s *Session) Snapshot(ctx context.Context, filter string, maxLines int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.ensureActive(ctx)
	if err != nil {
		return "", err
	}
	return s.snapshotLocked(ctx, t, filter, maxLines)
}

func (s *Session) snapshotLocked(ctx context.Context, t *sessionTab, filter string, maxLines int) (string, error) {
	title, url := s.titleURL(ctx, t)
	t.url = url // cache for origin-scoped approval (see CurrentOrigin)
	res, err := t.conn.Send(ctx, "Accessibility.getFullAXTree", nil)
	if err != nil {
		// Accessibility domain must be enabled on some builds.
		_, _ = t.conn.Send(ctx, "Accessibility.enable", nil)
		res, err = t.conn.Send(ctx, "Accessibility.getFullAXTree", nil)
		if err != nil {
			return "", err
		}
	}
	nodes, err := parseAXTree(res)
	if err != nil {
		return "", err
	}
	s.gen++
	var known map[int64]string
	if prev := s.snaps[t.conn.ID()]; prev != nil {
		known = prev.Refs
	}
	snap := buildSnapshot(nodes, filter, s.gen, maxLines, known, s.uidSeq)
	s.uidSeq = snap.NextUID
	s.snaps[t.conn.ID()] = snap

	header := fmt.Sprintf("[Page] %s — %s  (tab %s)", title, url, shortID(t.conn.ID()))
	body := snap.Text
	if body == "" {
		body = "(no interactive elements found; try filter=all or a screenshot)"
	}
	out := header + "\n" + body
	if d := t.dialog; d != nil {
		out += fmt.Sprintf("\n\n[dialog %s] %q — respond with browser_act action=dialog value=accept|dismiss", d.Type, d.Message)
	}
	return out, nil
}

func (s *Session) titleURL(ctx context.Context, t *sessionTab) (string, string) {
	res, err := t.conn.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "JSON.stringify({t:document.title,u:location.href})",
		"returnByValue": true,
	})
	if err != nil {
		return "", ""
	}
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(res, &r) != nil {
		return "", ""
	}
	var tu struct {
		T string `json:"t"`
		U string `json:"u"`
	}
	_ = json.Unmarshal([]byte(r.Result.Value), &tu)
	return tu.T, tu.U
}

// --- Screenshot ---

// Screenshot captures the active tab as PNG bytes.
func (s *Session) Screenshot(ctx context.Context, fullPage bool) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.ensureActive(ctx)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"format": "png"}
	if fullPage {
		params["captureBeyondViewport"] = true
	}
	res, err := t.conn.Send(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return nil, err
	}
	var r struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(r.Data)
}

// --- Read (console / network / text) ---

// PageText returns document.body innerText (bounded).
func (s *Session) PageText(ctx context.Context, limit int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.ensureActive(ctx)
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = 20000
	}
	res, err := t.conn.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    fmt.Sprintf("document.body ? document.body.innerText.slice(0,%d) : ''", limit),
		"returnByValue": true,
	})
	if err != nil {
		return "", err
	}
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return "", err
	}
	return r.Result.Value, nil
}

// Eval runs a read-only expression and returns its JSON value (dev mode gate is
// enforced by the tool/approval layer, not here).
func (s *Session) Eval(ctx context.Context, expr string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.ensureActive(ctx)
	if err != nil {
		return "", err
	}
	res, err := t.conn.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return "", err
	}
	var r struct {
		Result           json.RawMessage `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return "", err
	}
	if r.ExceptionDetails != nil {
		return "", fmt.Errorf("eval exception: %s", r.ExceptionDetails.Text)
	}
	return string(r.Result), nil
}

// --- Tabs ---

// ListTabs returns the tabs known to the backend, marking which are controlled.
func (s *Session) ListTabs(ctx context.Context) ([]TabInfo, error) {
	s.mu.Lock()
	controlled := make(map[string]bool, len(s.tabs))
	for id := range s.tabs {
		controlled[id] = true
	}
	active := s.active
	s.mu.Unlock()

	tabs, err := s.backend.ListTabs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range tabs {
		if controlled[tabs[i].ID] {
			tabs[i].Attached = true
		}
		_ = active
	}
	return tabs, nil
}

// NewTab opens a blank controlled tab.
func (s *Session) NewTab(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn, err := s.backend.NewTab(ctx, "about:blank")
	if err != nil {
		return "", err
	}
	s.registerTab(conn, true)
	return conn.ID(), nil
}

// SelectTab makes tabID active, attaching it if not yet controlled.
func (s *Session) SelectTab(ctx context.Context, tabID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tabID = s.resolveID(tabID)
	if _, ok := s.tabs[tabID]; ok {
		s.active = tabID
		return nil
	}
	conn, err := s.backend.AttachTab(ctx, tabID)
	if err != nil {
		return err
	}
	s.registerTab(conn, false)
	return nil
}

// ClaimTab takes control of a pre-existing (user) tab without closing it later.
func (s *Session) ClaimTab(ctx context.Context, tabID string) error {
	if err := s.SelectTab(ctx, tabID); err != nil {
		return err
	}
	s.mu.Lock()
	if t := s.tabs[s.resolveID(tabID)]; t != nil {
		t.created = false
	}
	s.mu.Unlock()
	return nil
}

// CloseTab closes a controlled tab.
func (s *Session) CloseTab(ctx context.Context, tabID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tabID = s.resolveID(tabID)
	t, ok := s.tabs[tabID]
	if !ok {
		return fmt.Errorf("tab %s not controlled", shortID(tabID))
	}
	err := t.conn.Close(ctx)
	delete(s.tabs, tabID)
	if s.active == tabID {
		s.active = ""
	}
	return err
}

// resolveID accepts a short id (first 8 chars) or full id.
func (s *Session) resolveID(id string) string {
	if _, ok := s.tabs[id]; ok {
		return id
	}
	for full := range s.tabs {
		if strings.HasPrefix(full, id) {
			return full
		}
	}
	return id
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
