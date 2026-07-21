// cloud_pairings.go implements the web pairing-approval endpoints (M11-W1):
// pending pairing requests arrive at the relay connector as pairing.request
// commands and are parked in its inbox; these endpoints list and resolve
// them (approve wraps the CEK for the requester via the M5 ECIES path, deny
// refuses). POST /api/cloud/pairing-offer mints a QR pairing offer (W3) and
// returns the jcode://pair URL the UI renders as a QR code.
package web

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/cnjack/jcode/internal/cloud"
)

// cloudPairingsResponse is the answer of GET /api/cloud/pairings. LastPaired
// lets the 5s-poller notice approvals (manual or QR auto-approve) and toast.
type cloudPairingsResponse struct {
	Pairings   []cloud.PendingPairing `json:"pairings"`
	LastPaired *cloud.PairedInfo      `json:"last_paired,omitempty"`
}

func (s *Server) pairingsSnapshot() cloudPairingsResponse {
	resp := cloudPairingsResponse{Pairings: []cloud.PendingPairing{}}
	if s.cloudSupervisor == nil {
		return resp
	}
	if list := s.cloudSupervisor.PendingPairings(); len(list) > 0 {
		resp.Pairings = list
	}
	if lp, ok := s.cloudSupervisor.LastPaired(); ok {
		resp.LastPaired = &lp
	}
	return resp
}

// handleCloudPairings serves GET /api/cloud/pairings.
func (s *Server) handleCloudPairings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.pairingsSnapshot())
}

// resolvePairing runs the supervisor's approve/deny for the {id} path value
// and maps the outcome to an HTTP status: 404 for an unknown/expired inbox
// entry, 503 when the relay is not connected, 500 for upstream failures.
func (s *Server) resolvePairing(w http.ResponseWriter, r *http.Request, approve bool) {
	if s.cloudSupervisor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cloud relay is not available"})
		return
	}
	id := r.PathValue("id")
	var err error
	if approve {
		err = s.cloudSupervisor.ApprovePairing(r.Context(), id)
	} else {
		err = s.cloudSupervisor.DenyPairing(r.Context(), id)
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, cloud.ErrUnknownPairing) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.pairingsSnapshot())
}

// handleCloudPairingApprove serves POST /api/cloud/pairings/{id}/approve.
func (s *Server) handleCloudPairingApprove(w http.ResponseWriter, r *http.Request) {
	s.resolvePairing(w, r, true)
}

// handleCloudPairingDeny serves POST /api/cloud/pairings/{id}/deny.
func (s *Server) handleCloudPairingDeny(w http.ResponseWriter, r *http.Request) {
	s.resolvePairing(w, r, false)
}

// cloudPairingOfferResponse is the answer of POST /api/cloud/pairing-offer:
// the jcode://pair URL to render as a QR code, plus its expiry for the
// countdown display.
type cloudPairingOfferResponse struct {
	QR        string `json:"qr"`
	OfferID   string `json:"offer_id"`
	ExpiresAt string `json:"expires_at"`
}

// handleCloudPairingOffer serves POST /api/cloud/pairing-offer: mints an
// offer at the orchestrator and builds the jcode://pair?cloud=…&device=…&offer=…&secret=…
// URL the mobile app scans to pair (W3 scan-to-pair).
func (s *Server) handleCloudPairingOffer(w http.ResponseWriter, r *http.Request) {
	creds, err := cloud.LoadCredentials()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if creds == nil || creds.DeviceToken == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not logged in"})
		return
	}
	offer, err := cloud.NewClient(creds.CloudURL).CreatePairingOffer(r.Context(), creds.DeviceToken)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to create pairing offer: " + err.Error()})
		return
	}
	q := url.Values{}
	q.Set("cloud", creds.CloudURL)
	q.Set("device", creds.DeviceID)
	q.Set("offer", offer.OfferID)
	q.Set("secret", offer.Secret)
	writeJSON(w, http.StatusOK, cloudPairingOfferResponse{
		QR:        "jcode://pair?" + q.Encode(),
		OfferID:   offer.OfferID,
		ExpiresAt: offer.ExpiresAt,
	})
}
