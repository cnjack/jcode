// Package toolpolicy defines transport-neutral execution-policy contracts.
package toolpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

const (
	ApprovalBillableExternal = "billable_external"
	CapabilityImageGenerate  = "image.generate"
	CapabilityWebSearch      = "web.search"
)

// BillableIntent is the immutable authorization and dispatch identity for one
// provider operation. It is prepared from the post-hook arguments, approved,
// and then consumed by the tool endpoint from the same context.
type BillableIntent struct {
	OperationID           string `json:"operation_id"`
	ToolCallID            string `json:"tool_call_id"`
	CapabilityKey         string `json:"capability_key"`
	Provider              string `json:"provider"`
	Model                 string `json:"model"`
	CredentialFingerprint string `json:"credential_fingerprint"`
	ConfigEpoch           string `json:"config_epoch"`
	NormalizedArgs        string `json:"normalized_args"`
	Count                 int    `json:"count"`
	IdempotencyKey        string `json:"idempotency_key"`
}

// NewOperationID returns a host-owned dispatch identity. Model-supplied tool
// call IDs are correlation labels and may repeat across turns or replays, so
// they must never key a durable quota journal.
func NewOperationID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate provider operation ID: %w", err)
	}
	return id.String(), nil
}

// BillableIntentPreparer is implemented by tools whose endpoint can create an
// externally billable side effect. The approval middleware calls it after
// PreToolUse rewrites and before any approval shortcut is evaluated.
type BillableIntentPreparer interface {
	PrepareBillableIntent(ctx context.Context, argsJSON, toolCallID string) (BillableIntent, error)
}

type intentContextKey struct{}

func WithBillableIntent(ctx context.Context, intent BillableIntent) context.Context {
	return context.WithValue(ctx, intentContextKey{}, intent)
}

func BillableIntentFromContext(ctx context.Context) (BillableIntent, bool) {
	if ctx == nil {
		return BillableIntent{}, false
	}
	intent, ok := ctx.Value(intentContextKey{}).(BillableIntent)
	return intent, ok && intent.OperationID != "" && intent.CapabilityKey != ""
}

// CanonicalJSON rejects unknown fields and trailing values, and returns a
// stable compact encoding suitable for binding an approval to arguments.
func CanonicalJSON(raw string, target any) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", fmt.Errorf("unexpected trailing JSON value")
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func Fingerprint(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

func StableID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:16])
}
