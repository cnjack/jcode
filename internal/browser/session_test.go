package browser

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// scriptedTab is a TabConn whose CDP responses come from a per-method script.
// It lets us drive Session logic without a real browser.
type scriptedTab struct {
	id       string
	mu       sync.Mutex
	resp     map[string]func(params any) json.RawMessage
	calls    []string
	h        EventHandler
	closed   int
	detached int
}

func newScriptedTab(id string) *scriptedTab {
	return &scriptedTab{id: id, resp: map[string]func(any) json.RawMessage{}}
}

func (t *scriptedTab) ID() string { return t.id }
func (t *scriptedTab) Send(_ context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	t.calls = append(t.calls, method)
	fn := t.resp[method]
	t.mu.Unlock()
	if fn != nil {
		return fn(params), nil
	}
	return json.RawMessage(`{}`), nil
}
func (t *scriptedTab) SetEventHandler(h EventHandler) { t.h = h }
func (t *scriptedTab) Close(context.Context) error {
	t.mu.Lock()
	t.closed++
	t.mu.Unlock()
	return nil
}
func (t *scriptedTab) Detach(context.Context) error {
	t.mu.Lock()
	t.detached++
	t.mu.Unlock()
	return nil
}

type fakeBackend struct {
	kind       string
	tab        *scriptedTab
	closeCalls int
}

func (b *fakeBackend) Kind() string                                    { return b.kind }
func (b *fakeBackend) NewTab(context.Context, string) (TabConn, error) { return b.tab, nil }
func (b *fakeBackend) ListTabs(context.Context) ([]TabInfo, error) {
	return []TabInfo{{ID: b.tab.id, Title: "T", URL: "https://x", Attached: false}}, nil
}
func (b *fakeBackend) AttachTab(context.Context, string) (TabConn, error) { return b.tab, nil }
func (b *fakeBackend) Close() error                                       { b.closeCalls++; return nil }

// axTreeJSON builds a getFullAXTree result with one link (backendId 101).
func axTreeJSON() json.RawMessage {
	tree := map[string]any{
		"nodes": []map[string]any{
			{"nodeId": "1", "role": map[string]any{"value": "RootWebArea"}, "name": map[string]any{"value": "Doc"}, "childIds": []string{"2"}},
			{"nodeId": "2", "role": map[string]any{"value": "link"}, "name": map[string]any{"value": "Files changed"}, "backendDOMNodeId": 101},
		},
	}
	b, _ := json.Marshal(tree)
	return b
}

func scriptedSession() (*Session, *scriptedTab) {
	tab := newScriptedTab("TARGET-abcdef123456")
	tab.resp["Accessibility.getFullAXTree"] = func(any) json.RawMessage { return axTreeJSON() }
	tab.resp["Runtime.evaluate"] = func(any) json.RawMessage {
		// titleURL and readyState both go through evaluate; return a value that
		// satisfies both parsers.
		return json.RawMessage(`{"result":{"value":"{\"t\":\"Doc\",\"u\":\"https://x/\"}"}}`)
	}
	tab.resp["DOM.getBoxModel"] = func(any) json.RawMessage {
		return json.RawMessage(`{"model":{"content":[10,10,20,10,20,20,10,20]}}`)
	}
	sess := NewSession(&fakeBackend{kind: "managed", tab: tab})
	return sess, tab
}

func TestSessionReloadIssuesPageReload(t *testing.T) {
	sess, tab := scriptedSession()
	// waitForLoad polls readyState (the scripted value never reads as "complete"),
	// so cancel up front to make it return on the first poll instead of the 6s
	// deadline — the mock ignores ctx for the actual Sends.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sess.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	var sawReload bool
	for _, m := range tab.calls {
		if m == "Page.reload" {
			sawReload = true
		}
	}
	if !sawReload {
		t.Errorf("Reload did not send Page.reload; calls=%v", tab.calls)
	}
}

func TestSessionCurrentOrigin(t *testing.T) {
	sess, _ := scriptedSession()
	// No active tab yet → no origin.
	if got := sess.CurrentOrigin(); got != "" {
		t.Errorf("origin before snapshot: got %q want empty", got)
	}
	if _, err := sess.Snapshot(context.Background(), "interactive", 100); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Snapshot caches the active tab URL (https://x/) → origin https://x.
	if got := sess.CurrentOrigin(); got != "https://x" {
		t.Errorf("origin after snapshot: got %q want https://x", got)
	}
}

func TestSessionSnapshotAndActFlow(t *testing.T) {
	sess, tab := scriptedSession()
	ctx := context.Background()

	out, err := sess.Snapshot(ctx, "interactive", 100)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !strings.Contains(out, `[e1] link "Files changed"`) {
		t.Fatalf("snapshot missing link:\n%s", out)
	}
	if !strings.Contains(out, "[Page] Doc") {
		t.Fatalf("snapshot missing header:\n%s", out)
	}

	// Act on the fresh uid → should resolve backend node 101 and click it.
	res, err := sess.Act(ctx, ActRequest{Action: "click", UID: "e1"})
	if err != nil {
		t.Fatalf("Act: %v", err)
	}
	if !strings.Contains(res, "ok: click e1") {
		t.Errorf("unexpected act result: %s", res)
	}
	// Verify a mouse event was actually dispatched.
	found := false
	for _, c := range tab.calls {
		if c == "Input.dispatchMouseEvent" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Input.dispatchMouseEvent, calls=%v", tab.calls)
	}
}

func TestSessionRejectsStaleUID(t *testing.T) {
	sess, _ := scriptedSession()
	ctx := context.Background()

	// Act before any snapshot → clear error.
	_, err := sess.Act(ctx, ActRequest{Action: "click", UID: "e1"})
	if err == nil || !strings.Contains(err.Error(), "no snapshot") {
		t.Fatalf("expected no-snapshot error, got %v", err)
	}

	// Take a snapshot, then reference a uid that was never minted.
	if _, err := sess.Snapshot(ctx, "interactive", 100); err != nil {
		t.Fatal(err)
	}
	_, err = sess.Act(ctx, ActRequest{Action: "click", UID: "e99"})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale-uid error, got %v", err)
	}
}

func TestSessionFillDispatchesInsertText(t *testing.T) {
	tab := newScriptedTab("T-1")
	tree := map[string]any{"nodes": []map[string]any{
		{"nodeId": "1", "role": map[string]any{"value": "RootWebArea"}, "name": map[string]any{"value": "Doc"}, "childIds": []string{"2"}},
		{"nodeId": "2", "role": map[string]any{"value": "textbox"}, "name": map[string]any{"value": "Comment"}, "backendDOMNodeId": 202},
	}}
	tb, _ := json.Marshal(tree)
	tab.resp["Accessibility.getFullAXTree"] = func(any) json.RawMessage { return tb }
	tab.resp["Runtime.evaluate"] = func(any) json.RawMessage {
		return json.RawMessage(`{"result":{"value":"{\"t\":\"Doc\",\"u\":\"https://x/\"}"}}`)
	}
	sess := NewSession(&fakeBackend{kind: "managed", tab: tab})
	ctx := context.Background()
	if _, err := sess.Snapshot(ctx, "interactive", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Act(ctx, ActRequest{Action: "fill", UID: "e1", Value: "hello"}); err != nil {
		t.Fatalf("fill: %v", err)
	}
	found := false
	for _, c := range tab.calls {
		if c == "Input.insertText" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Input.insertText, calls=%v", tab.calls)
	}
}

func TestSessionListTabsMarksControlled(t *testing.T) {
	sess, tab := scriptedSession()
	ctx := context.Background()
	// Create the active tab first.
	if _, err := sess.Snapshot(ctx, "interactive", 100); err != nil {
		t.Fatal(err)
	}
	tabs, err := sess.ListTabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var marked bool
	for _, ti := range tabs {
		if ti.ID == tab.id && ti.Attached {
			marked = true
		}
	}
	if !marked {
		t.Errorf("expected active tab marked attached: %+v", tabs)
	}
}

// TestSessionCloseKeepsBackend guards the P0 fix: Session.Close must never tear
// down the shared backend (managed Chrome / extension bridge), which the Manager
// reuses across tasks. It should only release this task's tabs.
func TestSessionCloseKeepsBackend(t *testing.T) {
	ctx := context.Background()

	// Managed backend, tab opened by the agent → closed on teardown, backend kept.
	created := newScriptedTab("target-created")
	mb := &fakeBackend{kind: "managed", tab: created}
	sess := NewSession(mb)
	if _, err := sess.NewTab(ctx); err != nil { // registers with created=true
		t.Fatal(err)
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	if mb.closeCalls != 0 {
		t.Errorf("Session.Close must not close the shared managed backend, got %d Close calls", mb.closeCalls)
	}
	if created.closed != 1 {
		t.Errorf("agent-created managed tab should be closed on teardown, got closed=%d", created.closed)
	}
	if created.detached != 0 {
		t.Errorf("agent-created managed tab should not be detached, got detached=%d", created.detached)
	}

	// Managed backend, tab claimed from another session → detached, not closed.
	claimed := newScriptedTab("target-claimed")
	mb2 := &fakeBackend{kind: "managed", tab: claimed}
	sess2 := NewSession(mb2)
	if err := sess2.ClaimTab(ctx, claimed.id); err != nil { // created=false
		t.Fatal(err)
	}
	if err := sess2.Close(); err != nil {
		t.Fatal(err)
	}
	if mb2.closeCalls != 0 {
		t.Errorf("Session.Close must not close the backend for a claimed tab, got %d", mb2.closeCalls)
	}
	if claimed.detached != 1 || claimed.closed != 0 {
		t.Errorf("claimed managed tab should be detached not closed, got detached=%d closed=%d", claimed.detached, claimed.closed)
	}

	// Extension backend: every tab lives in the user's real browser → hand back
	// via Detach, never Close, and never tear down the shared bridge.
	ext := newScriptedTab("ext-tab")
	eb := &fakeBackend{kind: "extension", tab: ext}
	esess := NewSession(eb)
	if _, err := esess.NewTab(ctx); err != nil {
		t.Fatal(err)
	}
	if err := esess.Close(); err != nil {
		t.Fatal(err)
	}
	if eb.closeCalls != 0 {
		t.Errorf("Session.Close must not close the shared extension backend, got %d", eb.closeCalls)
	}
	if ext.detached != 1 || ext.closed != 0 {
		t.Errorf("extension tab should be detached not closed, got detached=%d closed=%d", ext.detached, ext.closed)
	}
}
