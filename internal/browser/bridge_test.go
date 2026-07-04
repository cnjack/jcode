package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeExtension is a websocket client that acts like the jcode Chrome
// extension: it authenticates, then answers bridge requests from a script.
type fakeExtension struct {
	conn  *websocket.Conn
	token string
}

func dialExtension(t *testing.T, wsURL string, hello map[string]any) (*fakeExtension, bool) {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.WriteJSON(hello); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var resp map[string]any
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if resp["type"] == "error" {
		_ = conn.Close()
		return nil, false
	}
	tok, _ := resp["token"].(string)
	fe := &fakeExtension{conn: conn, token: tok}
	return fe, true
}

// serve answers bridge envelopes until the connection closes.
func (fe *fakeExtension) serve(handler func(env bridgeEnvelope) bridgeEnvelope) {
	go func() {
		for {
			var env bridgeEnvelope
			if err := fe.conn.ReadJSON(&env); err != nil {
				return
			}
			resp := handler(env)
			_ = fe.conn.WriteJSON(resp)
		}
	}()
}

func bridgeServer(t *testing.T) (*Bridge, string) {
	t.Helper()
	b := NewBridge()
	b.tokenPath = t.TempDir() + "/tokens.json" // isolate token persistence
	srv := httptest.NewServer(http.HandlerFunc(b.HandleWS))
	t.Cleanup(srv.Close)
	return b, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestBridgeTokenAuth(t *testing.T) {
	b, wsURL := bridgeServer(t)

	// A bad/absent token is rejected.
	if _, ok := dialExtension(t, wsURL, map[string]any{"type": "hello", "token": "nope"}); ok {
		t.Fatal("expected rejection for invalid token")
	}

	// A token issued by the server (Auto-connect path) authenticates.
	token := b.IssueToken()
	fe, ok := dialExtension(t, wsURL, map[string]any{"type": "hello", "token": token})
	if !ok {
		t.Fatal("issued token should connect")
	}
	if !b.Connected() {
		t.Fatal("bridge should report connected")
	}
	_ = fe.conn.Close()

	// The token persists and re-authenticates after reconnect.
	waitUntil(t, func() bool { return !b.Connected() })
	fe2, ok := dialExtension(t, wsURL, map[string]any{"type": "hello", "token": token})
	if !ok {
		t.Fatal("issued token should re-authenticate")
	}
	_ = fe2.conn.Close()
}

func TestBridgeCDPForwarding(t *testing.T) {
	b, wsURL := bridgeServer(t)
	token := b.IssueToken()
	fe, ok := dialExtension(t, wsURL, map[string]any{"type": "hello", "token": token})
	if !ok {
		t.Fatal("token auth failed")
	}
	// Script: tab.new → tabId; cdp.send Runtime.evaluate → echo result.
	fe.serve(func(env bridgeEnvelope) bridgeEnvelope {
		switch env.Type {
		case "tab.new":
			return bridgeEnvelope{Type: "tab.result", ID: env.ID, TabID: "chrome-tab-7"}
		case "tabs.list":
			return bridgeEnvelope{Type: "tabs.result", ID: env.ID, Tabs: []TabInfo{{ID: "chrome-tab-7", Title: "GH", URL: "https://github.com", UserTab: true}}}
		case "cdp.send":
			if env.Method == "Runtime.evaluate" {
				return bridgeEnvelope{Type: "cdp.result", ID: env.ID, Result: json.RawMessage(`{"result":{"value":"pong"}}`)}
			}
			return bridgeEnvelope{Type: "cdp.result", ID: env.ID, Result: json.RawMessage(`{}`)}
		}
		return bridgeEnvelope{Type: "cdp.result", ID: env.ID, Result: json.RawMessage(`{}`)}
	})

	waitUntil(t, b.Connected)
	backend, err := b.Backend()
	if err != nil {
		t.Fatalf("Backend: %v", err)
	}
	ctx := context.Background()

	tab, err := backend.NewTab(ctx, "https://github.com")
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	if tab.ID() != "chrome-tab-7" {
		t.Errorf("tab id = %q", tab.ID())
	}

	res, err := tab.Send(ctx, "Runtime.evaluate", map[string]any{"expression": "1"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(string(res), "pong") {
		t.Errorf("unexpected result: %s", res)
	}

	tabs, err := backend.ListTabs(ctx)
	if err != nil || len(tabs) != 1 || !tabs[0].UserTab {
		t.Fatalf("ListTabs: %v %+v", err, tabs)
	}
}

func TestBridgeStableTokenIsStable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBridge()

	tok1 := b.StableToken()
	if tok1 == "" {
		t.Fatal("expected a token")
	}
	// Same process: identical.
	if got := b.StableToken(); got != tok1 {
		t.Fatalf("stable token changed within process: %q vs %q", got, tok1)
	}
	// New bridge (simulates a restart): the persisted token is reused and valid.
	b2 := NewBridge()
	if got := b2.StableToken(); got != tok1 {
		t.Fatalf("stable token not reused across restart: %q vs %q", got, tok1)
	}
	if !b2.validToken(tok1) {
		t.Fatal("reused stable token should authenticate")
	}
}

func TestBridgeOfflineBackendErrors(t *testing.T) {
	b := NewBridge()
	b.tokenPath = t.TempDir() + "/tokens.json"
	if _, err := b.Backend(); err == nil {
		t.Fatal("expected error when no extension connected")
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
