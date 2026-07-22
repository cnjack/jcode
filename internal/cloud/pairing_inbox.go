// pairing_inbox.go handles pairing.request downlink commands (M11-W1): a
// request carrying an offer_id comes from a QR-code claim and is approved
// automatically (scan-to-pair); any other request is parked in an in-memory
// pending inbox until the user approves or denies it from the web UI (the CLI
// `jcode cloud approve|deny` path keeps working against the orchestrator
// directly). The inbox lives on the Connector because pairings only arrive
// through its poll loop; the Supervisor delegates the web endpoints to it.
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrUnknownPairing is returned by ApprovePairing/DenyPairing when the id is
// not in the connector's pending inbox (already handled, or never received).
var ErrUnknownPairing = errors.New("no such pending pairing")

// maxPendingPairings bounds the in-memory inbox; the oldest entry is dropped
// when the cap is hit (a flooded inbox must never grow unboundedly).
const maxPendingPairings = 32

// PendingPairing is one pairing.request parked for user approval. PubKey is
// kept in memory only (needed to wrap the CEK on approve) and never serialized.
type PendingPairing struct {
	PairingID  string    `json:"pairing_id"`
	Label      string    `json:"label"`
	ReceivedAt time.Time `json:"received_at"`
	PubKey     string    `json:"-"`
}

// PairedInfo records the most recent approval so the web UI can notify
// ("device X paired via QR code"). Auto is true for offer-based (QR scan)
// approvals, false for manual approvals.
type PairedInfo struct {
	PairingID string    `json:"pairing_id"`
	Label     string    `json:"label"`
	Auto      bool      `json:"auto"`
	PairedAt  time.Time `json:"paired_at"`
}

// pairingRequestPayload is the payload of a pairing.request command, as sent
// by the orchestrator when a client (console browser / mobile) asks to pair.
type pairingRequestPayload struct {
	PairingID string `json:"pairing_id"`
	Label     string `json:"label"`
	Kty       string `json:"kty"`    // "P-256"
	PubKey    string `json:"pubkey"` // requester P-256 public key, base64 SPKI DER
	OfferID   string `json:"offer_id,omitempty"`
}

// execPairingRequest handles a pairing.request command: an offer-carrying
// request is auto-approved (the QR scan IS the authorization), anything else
// is parked in the pending inbox. Either way the ack reports "ok"; only a
// malformed payload or a failed auto-approve surfaces as "error".
func (c *Connector) execPairingRequest(ctx context.Context, cmd DeviceCommand) (string, any) {
	var p pairingRequestPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return "error", map[string]string{"error": fmt.Sprintf("invalid pairing.request payload: %v", err)}
	}
	if p.PairingID == "" || p.PubKey == "" {
		return "error", map[string]string{"error": "pairing.request: pairing_id and pubkey are required"}
	}
	if p.OfferID != "" {
		if err := c.approvePairing(ctx, p.PairingID, p.Label, p.PubKey, true); err != nil {
			return "error", map[string]string{"error": fmt.Sprintf("auto-approve pairing %s: %v", p.PairingID, err)}
		}
		c.logf("pairing %s (%q) auto-approved via QR offer %s", p.PairingID, p.Label, p.OfferID)
		return "ok", map[string]string{"status": "approved", "auto": "true"}
	}
	c.addPendingPairing(p)
	c.logf("pairing %s (%q) parked for approval", p.PairingID, p.Label)
	return "ok", map[string]string{"status": "pending"}
}

// addPendingPairing parks a pairing request, replacing any previous entry
// with the same id and capping the inbox at maxPendingPairings.
func (c *Connector) addPendingPairing(p pairingRequestPayload) {
	c.pairMu.Lock()
	defer c.pairMu.Unlock()
	entry := PendingPairing{
		PairingID:  p.PairingID,
		Label:      p.Label,
		ReceivedAt: time.Now().UTC(),
		PubKey:     p.PubKey,
	}
	for i, e := range c.pending {
		if e.PairingID == p.PairingID {
			c.pending[i] = entry
			return
		}
	}
	if len(c.pending) >= maxPendingPairings {
		c.pending = c.pending[1:]
	}
	c.pending = append(c.pending, entry)
}

// PendingPairings snapshots the pending inbox (oldest first).
func (c *Connector) PendingPairings() []PendingPairing {
	c.pairMu.Lock()
	defer c.pairMu.Unlock()
	out := make([]PendingPairing, len(c.pending))
	copy(out, c.pending)
	return out
}

// LastPaired reports the most recent approval, ok=false when none happened
// since the connector started.
func (c *Connector) LastPaired() (PairedInfo, bool) {
	c.pairMu.Lock()
	defer c.pairMu.Unlock()
	if c.lastPaired == nil {
		return PairedInfo{}, false
	}
	return *c.lastPaired, true
}

// popPending removes id from the inbox, returning ErrUnknownPairing when
// absent.
func (c *Connector) popPending(id string) (PendingPairing, error) {
	c.pairMu.Lock()
	defer c.pairMu.Unlock()
	for i, e := range c.pending {
		if e.PairingID == id {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return e, nil
		}
	}
	return PendingPairing{}, fmt.Errorf("%w: %s", ErrUnknownPairing, id)
}

// ApprovePairing wraps the CEK for a pending requester and responds to the
// orchestrator (web endpoint path; manual approval).
func (c *Connector) ApprovePairing(ctx context.Context, id string) error {
	p, err := c.popPending(id)
	if err != nil {
		remote, getErr := c.client.GetPairing(ctx, c.token, id)
		if getErr != nil || remote.Status != "pending" {
			return err
		}
		p = PendingPairing{PairingID: remote.ID, Label: remote.Label, PubKey: remote.PubKey}
	}
	return c.approvePairing(ctx, p.PairingID, p.Label, p.PubKey, false)
}

// DenyPairing responds denial to the orchestrator for a pending request.
func (c *Connector) DenyPairing(ctx context.Context, id string) error {
	p, err := c.popPending(id)
	if err != nil {
		remote, getErr := c.client.GetPairing(ctx, c.token, id)
		if getErr != nil || remote.Status != "pending" {
			return err
		}
		p = PendingPairing{PairingID: remote.ID, Label: remote.Label, PubKey: remote.PubKey}
	}
	if err := c.client.RespondPairing(ctx, c.token, p.PairingID, false, 0, nil); err != nil {
		return fmt.Errorf("respond to pairing %s: %w", p.PairingID, err)
	}
	return nil
}

// approvePairing wraps the CEK for the requester pubkey and POSTs the
// approval, then records the last-paired notification. The CEK cipher is the
// connector's own when encryption is active, otherwise it is lazily
// initialized from the credentials file (same as `jcode cloud approve`).
func (c *Connector) approvePairing(ctx context.Context, id, label, pubKey string, auto bool) error {
	cipher := c.cipherSnapshot()
	if cipher == nil {
		var err error
		cipher, err = EnsureCEK()
		if err != nil {
			return err
		}
	}
	wrap, err := WrapCEK(pubKey, cipher.CEK(), cipher.KeyGen())
	if err != nil {
		return fmt.Errorf("wrap CEK: %w", err)
	}
	if err := c.client.RespondPairing(ctx, c.token, id, true, cipher.KeyGen(), wrap); err != nil {
		return fmt.Errorf("respond to pairing %s: %w", id, err)
	}
	c.pairMu.Lock()
	c.lastPaired = &PairedInfo{PairingID: id, Label: label, Auto: auto, PairedAt: time.Now().UTC()}
	// A manual approval of an inbox entry already popped it; an auto approval
	// may still find a duplicate parked entry — drop it either way.
	for i, e := range c.pending {
		if e.PairingID == id {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			break
		}
	}
	c.pairMu.Unlock()
	return nil
}

// PairingRecords returns the server-persisted audit trail. Unlike the pending
// inbox this survives desktop and connector restarts.
func (c *Connector) PairingRecords(ctx context.Context) ([]Pairing, error) {
	return c.client.ListPairings(ctx, c.token, "")
}

// RevokePairing rotates the account CEK and rewraps it for every approved
// client except id. The server commits target revocation, all replacement
// wraps, and devices.key_gen atomically.
func (c *Connector) RevokePairing(ctx context.Context, id string) error {
	// Block envelope sealing/opening for the short rekey transaction. Otherwise
	// an uplink could be encrypted with generation N after the server has
	// atomically advanced the device to generation N+1.
	c.cipherMu.Lock()
	defer c.cipherMu.Unlock()

	pairings, err := c.client.ListPairings(ctx, c.token, "approved")
	if err != nil {
		return fmt.Errorf("list approved pairings: %w", err)
	}
	found := false
	for i := range pairings {
		if pairings[i].ID == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrUnknownPairing, id)
	}
	oldCreds, err := LoadCredentials()
	if err != nil {
		return fmt.Errorf("load credentials before rekey: %w", err)
	}
	if oldCreds == nil {
		return fmt.Errorf("load credentials before rekey: not logged in")
	}
	rotated, err := RotateCEK()
	if err != nil {
		return fmt.Errorf("rotate CEK: %w", err)
	}
	wraps := make([]PairingRekeyWrap, 0, len(pairings)-1)
	for i := range pairings {
		if pairings[i].ID == id {
			continue
		}
		wrap, wrapErr := WrapCEK(pairings[i].PubKey, rotated.CEK(), rotated.KeyGen())
		if wrapErr != nil {
			_ = SaveCredentials(oldCreds)
			ResetCEKCache()
			return fmt.Errorf("rewrap CEK for %s: %w", pairings[i].ID, wrapErr)
		}
		wraps = append(wraps, PairingRekeyWrap{PairingID: pairings[i].ID, Wrap: wrap})
	}
	commitErr := c.client.RekeyPairings(ctx, c.token, id, rotated.KeyGen(), wraps)
	if commitErr != nil {
		var apiErr *APIError
		if !errors.As(commitErr, &apiErr) {
			// Transport/read failures have an unknown commit outcome. Retry the
			// idempotent transaction before deciding whether it is safe to roll back.
			commitErr = c.client.RekeyPairings(ctx, c.token, id, rotated.KeyGen(), wraps)
		}
		if commitErr != nil {
			state, stateErr := c.client.GetPairing(ctx, c.token, id)
			if stateErr == nil && state.Status == "revoked" && state.KeyGen == rotated.KeyGen() {
				c.cipher = rotated
				return nil
			}
			// A definitive API rejection, or a successful status read showing the
			// old approved generation, proves that the transaction did not commit.
			if errors.As(commitErr, &apiErr) || (stateErr == nil && state.Status == "approved") {
				if restoreErr := SaveCredentials(oldCreds); restoreErr != nil {
					return fmt.Errorf("commit rekey: %v; restore old CEK: %w", commitErr, restoreErr)
				}
				ResetCEKCache()
				return fmt.Errorf("commit rekey: %w", commitErr)
			}
			// Fail closed on a genuinely unknowable outcome: keep generation N+1
			// locally so a possibly revoked N key is never resurrected.
			c.cipher = rotated
			return fmt.Errorf("commit rekey outcome is unknown; kept generation %d locally: %w", rotated.KeyGen(), commitErr)
		}
	}
	c.cipher = rotated
	return nil
}
