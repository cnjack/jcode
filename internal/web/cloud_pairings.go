// cloud_pairings.go implements the web pairing-approval endpoints (M11-W1):
// pairing requests and their resolutions are persisted by cloud. These
// endpoints list the durable audit trail and allow desktop to approve, deny,
// or revoke. Revoke rotates the CEK, so the revoked client cannot continue to
// decrypt with an old locally cached key.
package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/cnjack/jcode/internal/cloud"
)

// cloudPairingsResponse is the answer of GET /api/cloud/pairings. LastPaired
// lets the 5s-poller notice approvals (manual or QR auto-approve) and toast.
type cloudPairingsResponse struct {
	Pairings   []cloudPairingRecord `json:"pairings"`
	LastPaired *cloud.PairedInfo    `json:"last_paired,omitempty"`
}

// cloudPairingRecord intentionally excludes the requester public key. The key
// is needed by desktop for wrapping but is not UI data.
type cloudPairingRecord struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	ResolvedAt string `json:"resolved_at,omitempty"`
}

func (s *Server) pairingsSnapshot(ctx context.Context) (cloudPairingsResponse, error) {
	resp := cloudPairingsResponse{Pairings: []cloudPairingRecord{}}
	if s.cloudSupervisor == nil {
		return resp, nil
	}
	list, err := s.cloudSupervisor.PairingRecords(ctx)
	if err != nil {
		return resp, err
	}
	for _, p := range list {
		resp.Pairings = append(resp.Pairings, cloudPairingRecord{
			ID: p.ID, Label: p.Label, Status: p.Status,
			CreatedAt: p.CreatedAt, ResolvedAt: p.ResolvedAt,
		})
	}
	if lp, ok := s.cloudSupervisor.LastPaired(); ok {
		resp.LastPaired = &lp
	}
	return resp, nil
}

// handleCloudPairings serves GET /api/cloud/pairings.
func (s *Server) handleCloudPairings(w http.ResponseWriter, r *http.Request) {
	resp, err := s.pairingsSnapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
	resp, snapshotErr := s.pairingsSnapshot(r.Context())
	if snapshotErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": snapshotErr.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleCloudPairingApprove serves POST /api/cloud/pairings/{id}/approve.
func (s *Server) handleCloudPairingApprove(w http.ResponseWriter, r *http.Request) {
	s.resolvePairing(w, r, true)
}

// handleCloudPairingDeny serves POST /api/cloud/pairings/{id}/deny.
func (s *Server) handleCloudPairingDeny(w http.ResponseWriter, r *http.Request) {
	s.resolvePairing(w, r, false)
}

// handleCloudPairingRevoke serves POST /api/cloud/pairings/{id}/revoke.
func (s *Server) handleCloudPairingRevoke(w http.ResponseWriter, r *http.Request) {
	if s.cloudSupervisor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cloud relay is not available"})
		return
	}
	err := s.cloudSupervisor.RevokePairing(r.Context(), r.PathValue("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, cloud.ErrUnknownPairing) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	resp, err := s.pairingsSnapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
