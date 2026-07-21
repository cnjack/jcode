package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

// GET /api/cloud/pairings mirrors the supervisor inbox (plus the last-paired
// notification); a nil supervisor yields an empty list.
func TestCloudPairingsList(t *testing.T) {
	received := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	fake := &fakeCloudSupervisor{
		pairings: []cloud.PendingPairing{
			{PairingID: "p1", Label: "jcode mobile", ReceivedAt: received, PubKey: "secret-key"},
		},
		lastPaired: &cloud.PairedInfo{PairingID: "p0", Label: "jcode android", Auto: true, PairedAt: received},
	}
	s := &Server{cfg: &config.Config{}, cloudSupervisor: fake}

	rec := httptest.NewRecorder()
	s.handleCloudPairings(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/pairings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp cloudPairingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Pairings) != 1 || resp.Pairings[0].PairingID != "p1" || resp.Pairings[0].Label != "jcode mobile" {
		t.Fatalf("pairings = %+v", resp.Pairings)
	}
	if resp.LastPaired == nil || resp.LastPaired.PairingID != "p0" || !resp.LastPaired.Auto {
		t.Fatalf("last_paired = %+v", resp.LastPaired)
	}
	// The requester pubkey must never leave the process.
	if strings.Contains(rec.Body.String(), "secret-key") {
		t.Fatalf("response leaks the requester pubkey: %s", rec.Body.String())
	}

	s = &Server{cfg: &config.Config{}}
	rec = httptest.NewRecorder()
	s.handleCloudPairings(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/pairings", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pairings":[]`) {
		t.Fatalf("nil supervisor: status=%d body=%s, want empty list", rec.Code, rec.Body.String())
	}
}

// Approve/deny delegate to the supervisor; unknown ids map to 404 and a
// missing relay to 503.
func TestCloudPairingApproveDeny(t *testing.T) {
	post := func(s *Server, path, id string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		if strings.HasSuffix(path, "/approve") {
			s.handleCloudPairingApprove(rec, req)
		} else {
			s.handleCloudPairingDeny(rec, req)
		}
		return rec
	}

	fake := &fakeCloudSupervisor{
		pairings: []cloud.PendingPairing{{PairingID: "p1", Label: "x", ReceivedAt: time.Now().UTC()}},
	}
	s := &Server{cfg: &config.Config{}, cloudSupervisor: fake}

	if rec := post(s, "/api/cloud/pairings/p1/approve", "p1"); rec.Code != http.StatusOK {
		t.Fatalf("approve: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := post(s, "/api/cloud/pairings/p2/deny", "p2"); rec.Code != http.StatusOK {
		t.Fatalf("deny: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.approved) != 1 || fake.approved[0] != "p1" || len(fake.denied) != 1 || fake.denied[0] != "p2" {
		t.Fatalf("supervisor calls: approved=%v denied=%v", fake.approved, fake.denied)
	}

	fake.pairingErr = cloud.ErrUnknownPairing
	if rec := post(s, "/api/cloud/pairings/gone/approve", "gone"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown pairing: status=%d, want 404", rec.Code)
	}

	s = &Server{cfg: &config.Config{}}
	if rec := post(s, "/api/cloud/pairings/p1/approve", "p1"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil supervisor approve: status=%d, want 503", rec.Code)
	}
}

// POST /api/cloud/pairing-offer mints an offer at the orchestrator and
// returns the jcode://pair URL for the QR code.
func TestCloudPairingOffer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/device/pairing-offers", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"offer_id":   "of-1",
			"secret":     "s3cret",
			"expires_at": "2026-07-21T04:00:00Z",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := cloud.SaveCredentials(&cloud.Credentials{
		CloudURL: srv.URL, DeviceID: "dev-9", DeviceToken: "tok", DeviceName: "box",
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	s.handleCloudPairingOffer(rec, httptest.NewRequest(http.MethodPost, "/api/cloud/pairing-offer", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("orchestrator Authorization = %q, want Bearer tok", gotAuth)
	}
	var resp cloudPairingOfferResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OfferID != "of-1" || resp.ExpiresAt != "2026-07-21T04:00:00Z" {
		t.Fatalf("offer response = %+v", resp)
	}
	u, err := url.Parse(resp.QR)
	if err != nil {
		t.Fatalf("qr URL: %v", err)
	}
	if u.Scheme != "jcode" || u.Host != "pair" {
		t.Fatalf("qr URL = %q, want jcode://pair?…", resp.QR)
	}
	q := u.Query()
	if q.Get("cloud") != srv.URL || q.Get("device") != "dev-9" || q.Get("offer") != "of-1" || q.Get("secret") != "s3cret" {
		t.Fatalf("qr query = %v", q)
	}
}

// The offer endpoint requires a logged-in device.
func TestCloudPairingOfferNotLoggedIn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	s.handleCloudPairingOffer(rec, httptest.NewRequest(http.MethodPost, "/api/cloud/pairing-offer", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
}
