// pairings.go implements the device-side pairing approval endpoints
// (M5 task book, device-token authenticated, under /internal/v1/device):
//
//	GET  /internal/v1/device/pairings?status=pending   → {pairings:[...]}
//	GET  /internal/v1/device/pairings/{id}             → pairing (incl. pubkey)
//	POST /internal/v1/device/pairings/{id}/respond     → {approve, wrap?}
//
// The pairing requester (console browser / mobile) generated a P-256 key pair
// and sent its SPKI DER base64 as `pubkey`; on approval the device wraps the
// CEK for that key (see crypto.go WrapCEK) and returns it via respond.
package cloud

import (
	"context"
	"fmt"
	"net/url"
)

// Pairing is one pairing request as seen by the device.
type Pairing struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	PubKey    string `json:"pubkey"` // requester P-256 public key, base64 SPKI DER
	Status    string `json:"status"` // "pending" | "approved" | "denied"
	CreatedAt string `json:"created_at"`
}

// ListPairings queries pairing requests, optionally filtered by status
// (empty = all): GET /internal/v1/device/pairings[?status=pending].
func (c *Client) ListPairings(ctx context.Context, token, status string) ([]Pairing, error) {
	path := "/internal/v1/device/pairings"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var out struct {
		Pairings []Pairing `json:"pairings"`
	}
	if _, err := c.get(ctx, path, token, &out); err != nil {
		return nil, err
	}
	return out.Pairings, nil
}

// GetPairing fetches one pairing request (including the requester pubkey):
// GET /internal/v1/device/pairings/{id}.
func (c *Client) GetPairing(ctx context.Context, token, id string) (*Pairing, error) {
	var out Pairing
	if _, err := c.get(ctx, "/internal/v1/device/pairings/"+url.PathEscape(id), token, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("pairing %s: empty response from %s", id, c.BaseURL)
	}
	return &out, nil
}

// RespondPairing approves or denies a pairing request:
// POST /internal/v1/device/pairings/{id}/respond. wrap carries the ECIES-
// wrapped CEK and is required on approval, nil on denial.
func (c *Client) RespondPairing(ctx context.Context, token, id string, approve bool, wrap *CEKWrap) error {
	body := struct {
		Approve bool     `json:"approve"`
		Wrap    *CEKWrap `json:"wrap,omitempty"`
	}{Approve: approve, Wrap: wrap}
	return c.post(ctx, "/internal/v1/device/pairings/"+url.PathEscape(id)+"/respond", token, body, nil)
}

// PairingOffer is the answer of POST /internal/v1/device/pairing-offers
// (M11-W3): a short-lived, single-use secret the device renders as a QR code;
// a mobile client claims it to create a pairing request flagged with the
// offer_id, which the device then auto-approves (scan-to-pair).
type PairingOffer struct {
	OfferID   string `json:"offer_id"`
	Secret    string `json:"secret"`
	ExpiresAt string `json:"expires_at"` // RFC 3339
}

// CreatePairingOffer mints a pairing offer:
// POST /internal/v1/device/pairing-offers (Bearer device token, empty body).
func (c *Client) CreatePairingOffer(ctx context.Context, token string) (*PairingOffer, error) {
	var out PairingOffer
	if err := c.post(ctx, "/internal/v1/device/pairing-offers", token, map[string]string{}, &out); err != nil {
		return nil, err
	}
	if out.OfferID == "" || out.Secret == "" {
		return nil, fmt.Errorf("incomplete pairing offer response from %s", c.BaseURL)
	}
	return &out, nil
}
