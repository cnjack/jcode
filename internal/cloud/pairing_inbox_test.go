package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/config"
)

// pairingMock is an httptest orchestrator that records pairing respond calls.
type pairingMock struct {
	t *testing.T

	respondCalls int
	lastApprove  bool
	lastWrap     *CEKWrap
	lastID       string
}

func newPairingMock(t *testing.T) (*pairingMock, *httptest.Server) {
	m := &pairingMock{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/device/pairings/{id}/respond", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("respond: Authorization = %q", r.Header.Get("Authorization"))
		}
		var body struct {
			Approve bool     `json:"approve"`
			Wrap    *CEKWrap `json:"wrap"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("respond: decode body: %v", err)
		}
		m.respondCalls++
		m.lastID = r.PathValue("id")
		m.lastApprove = body.Approve
		m.lastWrap = body.Wrap
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	return m, httptest.NewServer(mux)
}

func pairingRequestCmd(t *testing.T, id, label, pubKey, offerID string) DeviceCommand {
	t.Helper()
	payload := map[string]string{
		"pairing_id": id,
		"label":      label,
		"kty":        "P-256",
		"pubkey":     pubKey,
	}
	if offerID != "" {
		payload["offer_id"] = offerID
	}
	return DeviceCommand{ID: "cmd-" + id, Kind: "pairing.request", Payload: mustPayload(t, payload)}
}

// A pairing.request without an offer_id is parked in the pending inbox and
// acked ok; nothing is sent to the orchestrator.
func TestPairingRequestParksPending(t *testing.T) {
	pubB64, _ := p256Requester(t)
	mock, srv := newPairingMock(t)
	defer srv.Close()
	conn := newTestConnector(t, srv.URL, "http://127.0.0.1:1")

	status, result := conn.executeCommand(context.Background(), pairingRequestCmd(t, "p1", "jcode mobile", pubB64, ""))
	if status != "ok" {
		t.Fatalf("status = %q, result = %v", status, result)
	}
	if mock.respondCalls != 0 {
		t.Fatalf("respond called %d times, want 0 for a parked request", mock.respondCalls)
	}
	pending := conn.PendingPairings()
	if len(pending) != 1 || pending[0].PairingID != "p1" || pending[0].Label != "jcode mobile" {
		t.Fatalf("PendingPairings = %+v", pending)
	}
	if pending[0].ReceivedAt.IsZero() {
		t.Error("received_at must be set")
	}
	if pending[0].PubKey != pubB64 {
		t.Error("pubkey must be retained in memory for the later approve")
	}
	// The API serialization must not leak the requester pubkey.
	if data, _ := json.Marshal(pending[0]); strings.Contains(string(data), pubB64) {
		t.Errorf("marshaled PendingPairing leaks pubkey: %s", data)
	}
}

// A pairing.request carrying an offer_id (QR scan claim) is auto-approved:
// the CEK is wrapped for the requester and responded without user action.
func TestPairingRequestOfferAutoApproves(t *testing.T) {
	pubB64, priv := p256Requester(t)
	mock, srv := newPairingMock(t)
	defer srv.Close()

	conn := newTestConnector(t, srv.URL, "http://127.0.0.1:1")
	cek, err := GenerateCEK()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := NewEnvelopeCipher(cek, 1)
	if err != nil {
		t.Fatal(err)
	}
	conn.cipher = cipher

	status, result := conn.executeCommand(context.Background(), pairingRequestCmd(t, "p2", "jcode android", pubB64, "offer-1"))
	if status != "ok" {
		t.Fatalf("status = %q, result = %v", status, result)
	}
	if mock.respondCalls != 1 || !mock.lastApprove || mock.lastID != "p2" {
		t.Fatalf("respond calls = %d (id %q, approve %v), want 1 approve for p2", mock.respondCalls, mock.lastID, mock.lastApprove)
	}
	if mock.lastWrap == nil {
		t.Fatal("auto-approve respond must carry the CEK wrap")
	}
	// The requester can unwrap the CEK with its private key.
	gotCEK, keyGen, err := UnwrapCEK(mock.lastWrap, priv)
	if err != nil {
		t.Fatalf("UnwrapCEK: %v", err)
	}
	if keyGen != 1 || string(gotCEK) != string(cek) {
		t.Fatalf("unwrapped (key_gen=%d, cek match=%v), want key_gen=1 and the device CEK", keyGen, string(gotCEK) == string(cek))
	}
	// Nothing parked; the approval is recorded for the UI toast.
	if got := conn.PendingPairings(); len(got) != 0 {
		t.Fatalf("PendingPairings = %+v, want empty after auto-approve", got)
	}
	lp, ok := conn.LastPaired()
	if !ok || lp.PairingID != "p2" || lp.Label != "jcode android" || !lp.Auto {
		t.Fatalf("LastPaired = %+v, %v — want the auto approval recorded", lp, ok)
	}
}

// Manual approval from the web endpoint path wraps the CEK and responds;
// denial responds without a wrap; both consume the inbox entry.
func TestApproveDenyPairing(t *testing.T) {
	pubB64, priv := p256Requester(t)
	mock, srv := newPairingMock(t)
	defer srv.Close()

	conn := newTestConnector(t, srv.URL, "http://127.0.0.1:1")
	cek, err := GenerateCEK()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := NewEnvelopeCipher(cek, 2)
	if err != nil {
		t.Fatal(err)
	}
	conn.cipher = cipher

	ctx := context.Background()
	for _, id := range []string{"p1", "p2"} {
		if status, result := conn.executeCommand(ctx, pairingRequestCmd(t, id, "device "+id, pubB64, "")); status != "ok" {
			t.Fatalf("park %s: status = %q, result = %v", id, status, result)
		}
	}

	if err := conn.ApprovePairing(ctx, "p1"); err != nil {
		t.Fatalf("ApprovePairing: %v", err)
	}
	if mock.respondCalls != 1 || !mock.lastApprove || mock.lastWrap == nil {
		t.Fatalf("approve respond = calls %d approve %v wrap %v", mock.respondCalls, mock.lastApprove, mock.lastWrap != nil)
	}
	if _, keyGen, err := UnwrapCEK(mock.lastWrap, priv); err != nil || keyGen != 2 {
		t.Fatalf("UnwrapCEK = key_gen %d, err %v — want key_gen 2", keyGen, err)
	}
	lp, ok := conn.LastPaired()
	if !ok || lp.PairingID != "p1" || lp.Auto {
		t.Fatalf("LastPaired = %+v, %v — want manual approval of p1", lp, ok)
	}

	if err := conn.DenyPairing(ctx, "p2"); err != nil {
		t.Fatalf("DenyPairing: %v", err)
	}
	if mock.respondCalls != 2 || mock.lastApprove || mock.lastWrap != nil {
		t.Fatalf("deny respond = calls %d approve %v wrap %v", mock.respondCalls, mock.lastApprove, mock.lastWrap != nil)
	}

	if got := conn.PendingPairings(); len(got) != 0 {
		t.Fatalf("PendingPairings = %+v, want empty", got)
	}
	// Both ids are consumed — resolving them again reports unknown pairing.
	if err := conn.ApprovePairing(ctx, "p1"); !errors.Is(err, ErrUnknownPairing) {
		t.Fatalf("re-approve err = %v, want ErrUnknownPairing", err)
	}
	if err := conn.DenyPairing(ctx, "nope"); !errors.Is(err, ErrUnknownPairing) {
		t.Fatalf("deny unknown err = %v, want ErrUnknownPairing", err)
	}
}

// Malformed commands are acked error and touch nothing.
func TestPairingRequestInvalid(t *testing.T) {
	_, srv := newPairingMock(t)
	defer srv.Close()
	conn := newTestConnector(t, srv.URL, "http://127.0.0.1:1")

	if status, _ := conn.executeCommand(context.Background(), DeviceCommand{ID: "c", Kind: "pairing.request", Payload: mustPayload(t, map[string]string{"label": "x"})}); status != "error" {
		t.Fatalf("missing ids: status = %q, want error", status)
	}
	if status, _ := conn.executeCommand(context.Background(), DeviceCommand{ID: "c", Kind: "pairing.request", Payload: json.RawMessage(`{`)}); status != "error" {
		t.Fatalf("bad json: status = %q, want error", status)
	}
	if got := conn.PendingPairings(); len(got) != 0 {
		t.Fatalf("PendingPairings = %+v, want empty", got)
	}
}

// SyncCredentials starts the connector when credentials appear and stops it
// when they are removed (web login/logout hook).
func TestSupervisorSyncCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sup := NewSupervisor(&config.Config{}, 0, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup.Start(ctx)

	sup.mu.Lock()
	if sup.conn != nil {
		sup.mu.Unlock()
		t.Fatal("connector running without credentials")
	}
	sup.mu.Unlock()

	if err := SaveCredentials(&Credentials{
		CloudURL: "http://127.0.0.1:1", DeviceID: "dev-1", DeviceToken: "tok", DeviceName: "t",
	}); err != nil {
		t.Fatal(err)
	}
	sup.SyncCredentials()
	sup.mu.Lock()
	if sup.conn == nil {
		sup.mu.Unlock()
		t.Fatal("connector not started after credentials appeared")
	}
	sup.mu.Unlock()

	if err := DeleteCredentials(); err != nil {
		t.Fatal(err)
	}
	sup.SyncCredentials()
	sup.mu.Lock()
	if sup.conn != nil {
		sup.mu.Unlock()
		t.Fatal("connector still running after credentials removed")
	}
	sup.mu.Unlock()
	sup.Wait()
}
