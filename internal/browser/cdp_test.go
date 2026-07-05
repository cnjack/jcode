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

// fakeChrome is a websocket server that speaks just enough CDP for the tests:
// it echoes command results and can push events.
type fakeChrome struct {
	srv     *httptest.Server
	handler func(method string, params json.RawMessage) json.RawMessage
}

func newFakeChrome(t *testing.T, handler func(method string, params json.RawMessage) json.RawMessage) *fakeChrome {
	t.Helper()
	up := websocket.Upgrader{}
	fc := &fakeChrome{handler: handler}
	fc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			var msg cdpMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			result := fc.handler(msg.Method, msg.Params)
			if result == nil {
				result = json.RawMessage(`{}`)
			}
			_ = conn.WriteJSON(cdpMessage{ID: msg.ID, Result: result, SessionID: msg.SessionID})
		}
	}))
	t.Cleanup(fc.srv.Close)
	return fc
}

func (fc *fakeChrome) wsURL() string {
	return "ws" + strings.TrimPrefix(fc.srv.URL, "http")
}

func TestManagedBackendNewTabAndSend(t *testing.T) {
	fc := newFakeChrome(t, func(method string, params json.RawMessage) json.RawMessage {
		switch method {
		case "Target.createTarget":
			return json.RawMessage(`{"targetId":"T1"}`)
		case "Target.attachToTarget":
			return json.RawMessage(`{"sessionId":"S1"}`)
		case "Runtime.evaluate":
			return json.RawMessage(`{"result":{"value":"complete"}}`)
		}
		return json.RawMessage(`{}`)
	})

	ctx := context.Background()
	backend, err := connectManaged(ctx, fc.wsURL(), nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = backend.Close() }()

	tab, err := backend.NewTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	if tab.ID() != "T1" {
		t.Errorf("tab id = %q want T1", tab.ID())
	}
	res, err := tab.Send(ctx, "Runtime.evaluate", map[string]any{"expression": "1"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(string(res), "complete") {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestManagedBackendErrorPropagation(t *testing.T) {
	// Reply with a CDP error frame and immediately drop the socket. The error
	// frame and the connection-close then race inside send's select; the caller
	// must still surface the "boom" error rather than a "connection closed" one.
	// Loop so a regression (picking the close signal at random) fails reliably
	// instead of only ~half the time.
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		var msg cdpMessage
		_ = conn.ReadJSON(&msg)
		_ = conn.WriteJSON(cdpMessage{ID: msg.ID, Error: &cdpError{Code: -32000, Message: "boom"}})
		_ = conn.Close()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	for i := 0; i < 50; i++ {
		ctx := context.Background()
		backend, err := connectManaged(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		_, err = backend.cdp.send(ctx, "", "Target.getTargets", nil)
		_ = backend.Close()
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("iter %d: expected boom error, got %v", i, err)
		}
	}
}

func TestSendRespectsContextCancel(t *testing.T) {
	// Handler that never replies.
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := up.Upgrade(w, r, nil)
		var msg cdpMessage
		_ = conn.ReadJSON(&msg)
		time.Sleep(2 * time.Second)
		_ = conn.Close()
	}))
	defer srv.Close()

	backend, err := connectManaged(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = backend.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = backend.cdp.send(ctx, "", "Target.getTargets", nil)
	if err == nil {
		t.Fatal("expected context deadline error")
	}
}
