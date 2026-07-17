package computer

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestManagerRequestPermissionsWorksBeforeEnablement(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("permission requesting is a macOS helper feature")
	}
	var gotRequest requestPermissionsPayload
	fresh, _ := dialMock(t, func(d *mockDaemon) {
		d.on(typeRequestPermissions, func(id uint64, payload json.RawMessage) envelope {
			_ = json.Unmarshal(payload, &gotRequest)
			return result(id, pongPayload{
				ServerAPIVersion:          apiVersion,
				Platform:                  "darwin",
				AccessibilityPermission:   PermissionGranted,
				ScreenRecordingPermission: PermissionDenied,
			})
		})
	})
	// Enabled=false on purpose: the grants are a prerequisite for turning the
	// feature on, so the request must reach the helper without a session.
	mgr := NewManager(Config{}, t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.helperDialer = func(context.Context, string) (*helperBackend, error) { return fresh, nil }

	got, err := mgr.RequestPermissions(context.Background(), true, true)
	if err != nil {
		t.Fatalf("RequestPermissions: %v", err)
	}
	if !gotRequest.Accessibility || !gotRequest.ScreenRecording {
		t.Fatalf("request payload = %+v, want both grants requested", gotRequest)
	}
	if got.Accessibility != PermissionGranted || got.ScreenRecording != PermissionDenied {
		t.Fatalf("permissions = %+v, want granted/denied", got)
	}
}

func TestManagerSingleflightsConcurrentHelperInitialization(t *testing.T) {
	fresh, _ := dialMock(t, nil)
	mgr := NewManager(Config{Enabled: true, Backend: "helper"}, t.TempDir())

	started := make(chan struct{})
	release := make(chan struct{})
	var dialCalls atomic.Int32
	mgr.helperDialer = func(ctx context.Context, _ string) (*helperBackend, error) {
		if dialCalls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return fresh, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	const callers = 12
	start := make(chan struct{})
	results := make(chan *helperBackend, callers)
	errs := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			h, err := mgr.getHelper(context.Background())
			results <- h
			errs <- err
		}()
	}
	ready.Wait()
	close(start)
	<-started
	close(release)

	for range callers {
		if err := <-errs; err != nil {
			t.Fatalf("getHelper: %v", err)
		}
		if got := <-results; got != fresh {
			t.Fatalf("concurrent caller got helper %p, want shared helper %p", got, fresh)
		}
	}
	if got := dialCalls.Load(); got != 1 {
		t.Fatalf("concurrent initialization dialed %d helpers, want 1", got)
	}
}

func TestManagerStatusReportsConnectedRealHelper(t *testing.T) {
	helper, _ := dialMock(t, func(d *mockDaemon) {
		d.on(typePing, func(id uint64, payload json.RawMessage) envelope {
			return envelope{Type: typePong, ID: id, Payload: mustJSON(pongPayload{
				ServerAPIVersion:          apiVersion,
				Platform:                  "darwin",
				HelperVersion:             "status-test",
				AccessibilityPermission:   PermissionGranted,
				ScreenRecordingPermission: PermissionGranted,
			})}
		})
	})
	defer func() { _ = helper.Close() }()
	mgr := NewManager(Config{Enabled: true, Backend: "helper"}, t.TempDir())
	mgr.helper = helper

	status := mgr.Status(context.Background())
	if !status.Available || status.Blocker != "" || status.BackendKind != "helper" ||
		!status.Helper.Connected || status.Helper.Version != "status-test" ||
		status.AccessibilityPermission != PermissionGranted || status.ScreenRecordingPermission != PermissionGranted {
		t.Fatalf("connected helper status=%+v, want available helper without no_backend", status)
	}
}

func TestManagerStatusDoesNotTreatConnectedHelperWithoutBothPermissionsAsReady(t *testing.T) {
	helper, _ := dialMock(t, nil) // default: Accessibility granted, Screen Recording denied
	defer func() { _ = helper.Close() }()
	mgr := NewManager(Config{Enabled: true}, t.TempDir())
	mgr.helper = helper

	status := mgr.Status(context.Background())
	if status.Available || status.Blocker != "permissions" || !status.Helper.Connected ||
		status.AccessibilityPermission != PermissionGranted || status.ScreenRecordingPermission != PermissionDenied {
		t.Fatalf("partially permitted helper status=%+v, want permissions blocker", status)
	}
}

func TestManagerStatusTreatsUnknownPermissionProbeAsHelperProblem(t *testing.T) {
	helper, _ := dialMock(t, func(d *mockDaemon) {
		d.on(typePing, func(id uint64, _ json.RawMessage) envelope {
			return envelope{Type: typePong, ID: id, Payload: mustJSON(pongPayload{
				ServerAPIVersion: apiVersion,
				Platform:         "darwin",
				HelperVersion:    "legacy-without-permission-fields",
			})}
		})
	})
	defer func() { _ = helper.Close() }()
	mgr := NewManager(Config{Enabled: true}, t.TempDir())
	mgr.helper = helper

	status := mgr.Status(context.Background())
	if status.Available || status.Blocker != "no_helper" ||
		status.AccessibilityPermission != PermissionUnknown || status.ScreenRecordingPermission != PermissionUnknown {
		t.Fatalf("unknown permission probe status=%+v, want actionable helper blocker", status)
	}
}

func TestManagerStatusJSONMatchesSettingsContract(t *testing.T) {
	status := Status{
		AccessibilityPermission:   PermissionGranted,
		ScreenRecordingPermission: PermissionDenied,
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload["accessibility"] != string(PermissionGranted) ||
		payload["screen_recording"] != string(PermissionDenied) {
		t.Fatalf("status permission contract=%s", raw)
	}
	if _, legacy := payload["accessibility_permission"]; legacy {
		t.Fatalf("status leaked helper-protocol field names: %s", raw)
	}
}

func TestManagerIgnoresDeprecatedBackendSelector(t *testing.T) {
	sentinel := errors.New("helper dial attempted")
	for _, backend := range []string{"fake", "osa", "unknown"} {
		t.Run(backend, func(t *testing.T) {
			mgr := NewManager(Config{Enabled: true, Backend: backend}, t.TempDir())
			mgr.helperDialer = func(context.Context, string) (*helperBackend, error) {
				return nil, sentinel
			}
			if _, err := mgr.OpenSession(context.Background()); !errors.Is(err, sentinel) {
				t.Fatalf("Backend=%q selected something other than the production helper: %v", backend, err)
			}
		})
	}
}

func TestExplicitFakeInjectionDoesNotDependOnBackendSelector(t *testing.T) {
	for _, backend := range []string{"", "helper", "fake", "osa", "unknown"} {
		t.Run(backend, func(t *testing.T) {
			mgr := NewManager(Config{Enabled: true, Backend: backend}, t.TempDir())
			mgr.SetFakeBackend(NewFake())
			session, err := mgr.OpenSession(context.Background())
			if err != nil {
				t.Fatalf("OpenSession: %v", err)
			}
			if got := session.BackendKind(); got != "fake" {
				t.Fatalf("BackendKind=%q, want explicitly injected fake", got)
			}
		})
	}
}

func TestManagerPreapprovalUsesLiveConfig(t *testing.T) {
	mgr := NewManager(Config{
		Enabled:  true,
		Approval: map[string]string{"interact": "always_allow"},
	}, t.TempDir())
	if !mgr.Preapproved("com.apple.Notes", "interact") {
		t.Fatal("initial live default was not preapproved")
	}
	mgr.SetConfig(Config{
		Enabled: true,
		AppPermissions: []AppPermission{{
			BundleID: "com.apple.Notes",
			Interact: "ask",
		}},
	})
	if mgr.Preapproved("com.apple.Notes", "interact") {
		t.Fatal("hot config tightening retained a stale preapproval")
	}
	mgr.SetConfig(Config{Enabled: false, Approval: map[string]string{"interact": "always_allow"}})
	if mgr.Preapproved("com.apple.Notes", "interact") {
		t.Fatal("disabled manager still preapproved an interaction")
	}
}
