// mode_ceiling_test.go covers the M20 cloud mode ceiling: a cloud-originated
// chat.send asking for full_access (bypass) — under any alias — is refused at
// the protocol layer with ack error mode_not_allowed_for_cloud, before any
// local side effect. plan/approval/auto stay available on both dispatch paths.
package cloud

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestCloudForbiddenMode(t *testing.T) {
	forbidden := []string{"full_access", "FULL_ACCESS", " full_access ", "full-access", "fullaccess", "bypass", "bypass_permissions", "BypassPermissions"}
	for _, m := range forbidden {
		if !cloudForbiddenMode(m) {
			t.Errorf("cloudForbiddenMode(%q) = false, want true", m)
		}
	}
	allowed := []string{"", "approval", "plan", "auto", "build"}
	for _, m := range allowed {
		if cloudForbiddenMode(m) {
			t.Errorf("cloudForbiddenMode(%q) = true, want false", m)
		}
	}
}

// The ceiling fires on BOTH dispatch paths (legacy one-shot and M12 compose)
// and leaves no local side effects.
func TestChatSendModeCeilingRejected(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{"legacy full_access", map[string]any{"text": "hi", "mode": "full_access"}},
		{"compose full_access", map[string]any{"text": "hi", "mode": "full_access", "project_path": "/tmp/p"}},
		{"compose bypass alias", map[string]any{"text": "hi", "mode": "bypass", "goal": "g"}},
		{"compose uppercase", map[string]any{"text": "hi", "mode": "FULL_ACCESS", "effort": "low"}},
		// goal_armed ignores mode — but a payload declaring the intent is
		// refused before the goal endpoint is touched.
		{"goal_armed full_access", map[string]any{"text": "obj", "mode": "full_access", "goal_armed": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			local, localSrv := newFakeComposeLocal(t)
			conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)
			cmd := DeviceCommand{ID: "cmd-ceiling", Kind: "chat.send", Payload: mustPayload(t, tc.payload)}
			status, result := conn.executeCommand(context.Background(), cmd)
			if status != "error" {
				t.Fatalf("status = %q, result = %v; want error", status, result)
			}
			if !strings.Contains(fmt.Sprint(result), "mode_not_allowed_for_cloud") {
				t.Fatalf("result = %v, want mode_not_allowed_for_cloud", result)
			}
			if calls, _ := local.snapshot(); len(calls) != 0 {
				t.Fatalf("local calls = %v, want none (no side effects on a rejected mode)", calls)
			}
		})
	}
}

// plan/approval/auto pass the ceiling: the legacy path forwards the mode to
// /api/chat; the compose path switches it on the focused engine via /api/mode.
func TestChatSendModeCeilingAllowed(t *testing.T) {
	for _, m := range []string{"approval", "plan", "auto"} {
		t.Run("legacy "+m, func(t *testing.T) {
			local, localSrv := newFakeLocal(t)
			conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)
			cmd := DeviceCommand{
				ID:      "cmd-ok",
				Kind:    "chat.send",
				Payload: mustPayload(t, map[string]any{"text": "hi", "mode": m}),
			}
			status, result := conn.executeCommand(context.Background(), cmd)
			if status != "ok" {
				t.Fatalf("status = %q, result = %v", status, result)
			}
			if got := local.chatBody()["mode"]; got != m {
				t.Errorf("chat mode = %v, want %q", got, m)
			}
		})
		t.Run("compose "+m, func(t *testing.T) {
			local, localSrv := newFakeComposeLocal(t)
			conn := newTestConnector(t, "http://127.0.0.1:1", localSrv.URL)
			cmd := DeviceCommand{
				ID:      "cmd-ok-compose",
				Kind:    "chat.send",
				Payload: mustPayload(t, map[string]any{"text": "hi", "mode": m, "project_path": "/tmp/p"}),
			}
			status, result := conn.executeCommand(context.Background(), cmd)
			if status != "ok" {
				t.Fatalf("status = %q, result = %v", status, result)
			}
			_, bodies := local.snapshot()
			if got := bodies["/api/mode"][0]["mode"]; got != m {
				t.Errorf("/api/mode body = %v, want mode %q", bodies["/api/mode"], m)
			}
		})
	}
}
