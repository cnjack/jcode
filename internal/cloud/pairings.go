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
	ID         string `json:"id"`
	Label      string `json:"label"`
	PubKey     string `json:"pubkey"` // requester P-256 public key, base64 SPKI DER
	Status     string `json:"status"` // pending | approved | denied | expired | revoked
	KeyGen     int    `json:"key_gen,omitempty"`
	CreatedAt  string `json:"created_at"`
	ResolvedAt string `json:"resolved_at,omitempty"`
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
func (c *Client) RespondPairing(ctx context.Context, token, id string, approve bool, keyGen int, wrap *CEKWrap) error {
	body := struct {
		Approve bool     `json:"approve"`
		KeyGen  int      `json:"key_gen,omitempty"`
		Wrap    *CEKWrap `json:"wrap,omitempty"`
	}{Approve: approve, KeyGen: keyGen, Wrap: wrap}
	return c.post(ctx, "/internal/v1/device/pairings/"+url.PathEscape(id)+"/respond", token, body, nil)
}

// PairingRekeyWrap replaces one approved client's opaque CEK wrap.
type PairingRekeyWrap struct {
	PairingID string   `json:"pairing_id"`
	Wrap      *CEKWrap `json:"wrap"`
}

// RekeyPairings atomically revokes one client, advances the device key
// generation, and installs generation-N+1 wraps for every remaining client.
func (c *Client) RekeyPairings(ctx context.Context, token, revokedID string, keyGen int, wraps []PairingRekeyWrap) error {
	body := struct {
		RevokePairingID string             `json:"revoke_pairing_id"`
		KeyGen          int                `json:"key_gen"`
		Wraps           []PairingRekeyWrap `json:"wraps"`
	}{RevokePairingID: revokedID, KeyGen: keyGen, Wraps: wraps}
	return c.post(ctx, "/internal/v1/device/pairings/rekey", token, body, nil)
}
