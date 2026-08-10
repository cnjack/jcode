// Package responsemeta defines the small, provider-neutral contract used to
// carry stateless Responses API continuation data through Eino messages and
// the JSONL session recorder.
package responsemeta

import (
	"encoding/json"
)

const (
	// OpaqueItemsExtraKey is the stable schema.Message.Extra key used by the
	// Responses transport, runner, and session replay.
	OpaqueItemsExtraKey = "jcode.responses.opaque_items"

	// MaxOpaqueItems bounds encrypted reasoning items retained per message.
	MaxOpaqueItems = 16
	// MaxOpaqueItemBytes bounds one canonical encrypted reasoning item.
	MaxOpaqueItemBytes = 512 << 10
	// MaxOpaqueItemsBytes bounds all encrypted items retained per message.
	MaxOpaqueItemsBytes  = 2 << 20
	maxOpaqueItemIDBytes = 512
)

type reasoningItem struct {
	Type             string            `json:"type"`
	ID               string            `json:"id,omitempty"`
	Summary          []json.RawMessage `json:"summary"`
	EncryptedContent string            `json:"encrypted_content"`
}

// CanonicalReasoningItem accepts only an encrypted Responses reasoning item
// and strips every provider-returned cleartext field. Summary is deliberately
// serialized as an empty array because the Codex Responses schema requires it.
func CanonicalReasoningItem(raw []byte) (json.RawMessage, bool) {
	if len(raw) == 0 || len(raw) > MaxOpaqueItemBytes {
		return nil, false
	}
	var item reasoningItem
	if err := json.Unmarshal(raw, &item); err != nil || item.Type != "reasoning" ||
		item.EncryptedContent == "" || len(item.EncryptedContent) > MaxOpaqueItemBytes ||
		len(item.ID) > maxOpaqueItemIDBytes {
		return nil, false
	}
	item.Summary = []json.RawMessage{}
	canonical, err := json.Marshal(item)
	if err != nil || len(canonical) > MaxOpaqueItemBytes {
		return nil, false
	}
	return canonical, true
}

// Normalize returns a bounded, canonical collection. It is safe to call on
// untrusted session data: invalid, cleartext-only, oversized, and excess items
// are ignored.
func Normalize(items []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, min(len(items), MaxOpaqueItems))
	total := 0
	for _, raw := range items {
		if len(out) >= MaxOpaqueItems {
			break
		}
		item, ok := CanonicalReasoningItem(raw)
		if !ok || total > MaxOpaqueItemsBytes-len(item) {
			continue
		}
		total += len(item)
		out = append(out, item)
	}
	return out
}

// FromExtra extracts canonical opaque items from the runtime message value.
// The transport and session replay use []json.RawMessage, while []any support
// keeps the contract resilient to a JSON marshal/unmarshal boundary.
func FromExtra(extra map[string]any) []json.RawMessage {
	if len(extra) == 0 {
		return nil
	}
	value, ok := extra[OpaqueItemsExtraKey]
	if !ok {
		return nil
	}
	var items []json.RawMessage
	switch typed := value.(type) {
	case []json.RawMessage:
		items = typed
	case json.RawMessage:
		items = []json.RawMessage{typed}
	case []byte:
		items = []json.RawMessage{typed}
	case string:
		items = []json.RawMessage{json.RawMessage(typed)}
	case []any:
		items = make([]json.RawMessage, 0, len(typed))
		for _, item := range typed {
			raw, err := json.Marshal(item)
			if err == nil {
				items = append(items, raw)
			}
		}
	default:
		raw, err := json.Marshal(typed)
		if err == nil {
			items = []json.RawMessage{raw}
		}
	}
	return Normalize(items)
}

// Extra builds the canonical message metadata map for opaque items.
func Extra(items []json.RawMessage) map[string]any {
	items = Normalize(items)
	if len(items) == 0 {
		return nil
	}
	return map[string]any{OpaqueItemsExtraKey: items}
}
