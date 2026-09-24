// Package modelcatalog projects the many vendor dialects of an
// OpenAI-compatible GET /models response onto one provider-neutral shape.
//
// The OpenAI reference payload only carries ids, but real gateways add rich
// metadata in incompatible places: GitHub Copilot (and Copilot relays) nest it
// under capabilities.limits / capabilities.supports, OpenRouter uses
// context_length + architecture + supported_parameters, Mistral and LM Studio
// expose capability flags or lists, vLLM reports max_model_len, LiteLLM uses
// max_input_tokens, and so on. Parse understands all of these so a custom
// endpoint's catalog can carry real context windows, vision, tool and
// reasoning-effort support instead of bare ids.
package modelcatalog

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Kind is the normalized workload a catalog entry serves.
type Kind string

// Known kinds. KindUnknown means the endpoint did not say; callers treat it as
// chat because the OpenAI reference payload carries no type at all.
const (
	KindUnknown    Kind = ""
	KindChat       Kind = "chat"
	KindEmbedding  Kind = "embedding"
	KindImage      Kind = "image"
	KindVideo      Kind = "video"
	KindAudio      Kind = "audio"
	KindRerank     Kind = "rerank"
	KindModeration Kind = "moderation"
)

// Entry is one model advertised by a /models endpoint. Tri-state capability
// flags are nil when the endpoint did not advertise them, so callers can fill
// only the unknowns from another source (e.g. the built-in registry) without
// overriding what the endpoint explicitly declared.
type Entry struct {
	ID     string
	Name   string // display name; empty when the endpoint advertises none
	Vendor string
	Kind   Kind
	// Context is the usable prompt budget in tokens (0 = unknown). When an
	// endpoint distinguishes the prompt limit from the total window (Copilot's
	// max_prompt_tokens vs max_context_window_tokens) the prompt limit wins:
	// compaction math treats this value as the input ceiling, and requests
	// above max_prompt_tokens are rejected even when the window is larger.
	Context     int
	Attachment  *bool // accepts image input
	ToolCall    *bool
	Reasoning   *bool
	EffortTiers []string // advertised reasoning_effort values, in endpoint order
	// Hidden mirrors Copilot's model_picker_enabled=false: listed for API
	// completeness but not meant to be offered in a model picker.
	Hidden bool
}

// IsChat reports whether the entry can serve chat completions. Unknown kinds
// count as chat because plain OpenAI-compatible payloads carry no type.
func (e Entry) IsChat() bool {
	return e.Kind == KindUnknown || e.Kind == KindChat
}

// Selectable reports whether the entry belongs in a coding-agent model
// picker: a visible chat model that has not explicitly declared that it
// cannot call tools.
func (e Entry) Selectable() bool {
	return !e.Hidden && e.IsChat() && (e.ToolCall == nil || *e.ToolCall)
}

// Decode parses a raw /models response body. Numbers are decoded as
// json.Number so large token limits survive exactly.
func Decode(body []byte) ([]Entry, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	return Parse(payload), nil
}

// Parse extracts entries from an already-decoded /models payload. Entries
// keep the endpoint's order; duplicates (by id) keep the first occurrence.
func Parse(payload any) []Entry {
	raw := RawEntries(payload)
	entries := make([]Entry, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		entry, ok := ParseEntry(item.Value, item.FallbackID)
		if !ok {
			continue
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			continue
		}
		seen[entry.ID] = struct{}{}
		entries = append(entries, entry)
	}
	return entries
}

// RawEntry is one undecoded element of a /models listing. FallbackID carries
// the map key when the listing is an id-keyed object ({"models":{"id":{...}}}).
type RawEntry struct {
	Value      any
	FallbackID string
}

// RawEntries locates the model list inside the common response envelopes: a
// bare array, {"data":[...]}, {"items":[...]}, {"models":[...]} or an id-keyed
// {"models":{...}} map.
func RawEntries(payload any) []RawEntry {
	if list, ok := payload.([]any); ok {
		return wrapRawEntries(list)
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"data", "items", "models"} {
		if list, ok := object[key].([]any); ok {
			return wrapRawEntries(list)
		}
	}
	if modelMap, ok := object["models"].(map[string]any); ok {
		entries := make([]RawEntry, 0, len(modelMap))
		for id, value := range modelMap {
			entries = append(entries, RawEntry{Value: value, FallbackID: id})
		}
		return entries
	}
	return nil
}

func wrapRawEntries(list []any) []RawEntry {
	entries := make([]RawEntry, 0, len(list))
	for _, value := range list {
		entries = append(entries, RawEntry{Value: value})
	}
	return entries
}

// ParseEntry projects a single listing element. Plain strings are treated as
// ids. It returns false when no id can be determined.
func ParseEntry(value any, fallbackID string) (Entry, bool) {
	if id, ok := value.(string); ok {
		id = strings.TrimSpace(id)
		return Entry{ID: id}, id != ""
	}
	object, ok := value.(map[string]any)
	if !ok {
		id := strings.TrimSpace(fallbackID)
		return Entry{ID: id}, id != ""
	}

	id := firstString(object, "slug", "id", "model")
	if id == "" {
		id = strings.TrimSpace(fallbackID)
	}
	if id == "" {
		id = firstString(object, "name")
	}
	if id == "" {
		return Entry{}, false
	}
	entry := Entry{
		ID:     id,
		Name:   firstString(object, "name", "display_name", "displayName", "label", "title"),
		Vendor: firstString(object, "vendor", "owned_by", "ownedBy", "provider", "owner", "organization", "publisher"),
		Kind:   parseKind(object),
	}
	if entry.Name == entry.ID {
		entry.Name = ""
	}
	if picker, ok := object["model_picker_enabled"].(bool); ok && !picker {
		entry.Hidden = true
	}
	entry.Context = parseContext(object)

	caps := capabilityList(object)
	supports := nestedMap(object, "capabilities", "supports")
	if entry.Kind == KindUnknown && caps.has("embedding", "embeddings") && !caps.has("completion", "chat") {
		entry.Kind = KindEmbedding
	}
	entry.Attachment = parseAttachment(object, supports, caps, entry.Kind)
	entry.ToolCall = parseToolCall(object, supports, caps)
	entry.EffortTiers = parseEffortTiers(object, supports)
	entry.Reasoning = parseReasoning(object, supports, caps, entry.EffortTiers)
	return entry, true
}

// contextPaths lists token-limit locations in priority order: explicit prompt
// budgets first, total context windows second.
var contextPaths = [][]string{
	{"capabilities", "limits", "max_prompt_tokens"},
	{"max_prompt_tokens"},
	{"max_input_tokens"},
	{"input_token_limit"},
	{"inputTokenLimit"},
	{"model_info", "max_input_tokens"},
	{"capabilities", "limits", "max_context_window_tokens"},
	{"context_length"},
	{"context_window"},
	{"max_context_length"},
	{"max_context_window"},
	{"max_context_window_tokens"},
	{"max_context_tokens"},
	{"max_model_len"},
	{"context_size"},
	{"top_provider", "context_length"},
	{"limits", "max_prompt_tokens"},
	{"limits", "context"},
	{"limits", "context_window"},
	{"limits", "max_context_window_tokens"},
	{"limit", "context"},
	{"metadata", "context_length"},
	{"metadata", "context_window"},
}

func parseContext(object map[string]any) int {
	for _, path := range contextPaths {
		if value, ok := positiveInt(lookup(object, path...)); ok {
			return value
		}
	}
	return 0
}

func parseKind(object map[string]any) Kind {
	for _, path := range [][]string{
		{"capabilities", "type"}, {"type"}, {"model_type"}, {"mode"}, {"task"},
	} {
		if text, ok := lookup(object, path...).(string); ok {
			if kind := normalizeKind(text); kind != KindUnknown {
				return kind
			}
		}
	}
	if outputs, ok := stringList(lookup(object, "architecture", "output_modalities")); ok && len(outputs) > 0 {
		return kindFromOutputs(outputs)
	}
	if modality, ok := lookup(object, "architecture", "modality").(string); ok {
		if _, output, found := strings.Cut(modality, "->"); found {
			return kindFromOutputs(strings.Split(output, "+"))
		}
	}
	return KindUnknown
}

func normalizeKind(text string) Kind {
	normalized := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(text)))
	switch normalized {
	case "chat", "llm", "vlm", "text", "language", "completion", "completions",
		"chat_completion", "chat_completions", "text_generation", "code", "responses",
		"instruct", "conversational":
		return KindChat
	case "embedding", "embeddings", "text_embedding", "embed":
		return KindEmbedding
	case "image", "images", "image_generation", "text_to_image", "image_gen", "diffusion":
		return KindImage
	case "video", "video_generation", "text_to_video":
		return KindVideo
	case "audio", "tts", "stt", "speech", "transcription", "audio_transcription",
		"audio_speech", "text_to_speech", "speech_to_text":
		return KindAudio
	case "rerank", "reranker", "reranking":
		return KindRerank
	case "moderation", "moderations":
		return KindModeration
	default:
		return KindUnknown
	}
}

// kindFromOutputs maps OpenRouter-style output modalities to a kind. Any text
// output makes the model a chat model; otherwise the first media type wins.
func kindFromOutputs(outputs []string) Kind {
	first := KindUnknown
	for _, output := range outputs {
		var kind Kind
		switch strings.ToLower(strings.TrimSpace(output)) {
		case "text":
			return KindChat
		case "image":
			kind = KindImage
		case "video":
			kind = KindVideo
		case "audio":
			kind = KindAudio
		case "embedding", "embeddings":
			kind = KindEmbedding
		}
		if first == KindUnknown {
			first = kind
		}
	}
	return first
}

func parseAttachment(object, supports map[string]any, caps capabilitySet, kind Kind) *bool {
	if kind != KindUnknown && kind != KindChat {
		return boolPtr(false)
	}
	if supports != nil {
		if flag, ok := supports["vision"].(bool); ok {
			return boolPtr(flag)
		}
		return boolPtr(nestedMap(object, "capabilities", "limits", "vision") != nil)
	}
	if strings.EqualFold(firstString(object, "type"), "vlm") || caps.has("vision", "image", "image_input") {
		return boolPtr(true)
	}
	if price, ok := positiveNumber(object["prompt_image_token_price"]); ok && price > 0 {
		return boolPtr(true)
	}
	if flag, ok := firstBool(object,
		[]string{"supports_vision"}, []string{"supports_image_input"}, []string{"supports_images"},
		[]string{"supports_image"}, []string{"vision"}, []string{"image_input"},
		[]string{"capabilities", "vision"}, []string{"capabilities", "image_input"},
	); ok {
		return boolPtr(flag)
	}
	for _, path := range [][]string{
		{"architecture", "input_modalities"}, {"input_modalities"}, {"modalities", "input"},
		{"modalities", "input_modalities"}, {"modalities"},
	} {
		if inputs, ok := stringList(lookup(object, path...)); ok && len(inputs) > 0 {
			return boolPtr(containsFold(inputs, "image"))
		}
	}
	if modality, ok := lookup(object, "architecture", "modality").(string); ok {
		if input, _, found := strings.Cut(modality, "->"); found {
			return boolPtr(containsFold(strings.Split(input, "+"), "image"))
		}
	}
	return nil
}

func parseToolCall(object, supports map[string]any, caps capabilitySet) *bool {
	if supports != nil {
		if flag, ok := supports["tool_calls"].(bool); ok {
			return boolPtr(flag)
		}
	}
	if flag, ok := firstBool(object,
		[]string{"supports_tools"}, []string{"supports_function_calling"}, []string{"supports_tool_calls"},
		[]string{"supports_tool_use"}, []string{"function_calling"}, []string{"tool_calls"},
		[]string{"tool_use"}, []string{"tools"},
		[]string{"capabilities", "function_calling"}, []string{"capabilities", "tools"},
		[]string{"capabilities", "tool_calls"}, []string{"capabilities", "tool_use"},
	); ok {
		return boolPtr(flag)
	}
	if caps.has("tools", "tool_use", "tool_calls", "function_calling") {
		return boolPtr(true)
	}
	if params, ok := stringList(object["supported_parameters"]); ok && len(params) > 0 {
		return boolPtr(containsFold(params, "tools") || containsFold(params, "tool_choice"))
	}
	return nil
}

func parseReasoning(object, supports map[string]any, caps capabilitySet, tiers []string) *bool {
	if len(tiers) > 0 {
		return boolPtr(true)
	}
	if flag, ok := firstBool(object,
		[]string{"supports_reasoning"}, []string{"reasoning"}, []string{"supports_thinking"},
		[]string{"thinking"}, []string{"capabilities", "reasoning"}, []string{"capabilities", "thinking"},
		[]string{"capabilities", "supports", "reasoning"},
	); ok {
		return boolPtr(flag)
	}
	if caps.has("thinking", "reasoning") {
		return boolPtr(true)
	}
	if params, ok := stringList(object["supported_parameters"]); ok && len(params) > 0 {
		return boolPtr(containsFold(params, "reasoning") || containsFold(params, "include_reasoning") ||
			containsFold(params, "reasoning_effort"))
	}
	// Copilot-style supports blocks advertise reasoning_effort explicitly. A
	// thinking budget alone is a different wire parameter, so it must not
	// light up the reasoning_effort control.
	if supports != nil {
		return boolPtr(false)
	}
	return nil
}

func parseEffortTiers(object, supports map[string]any) []string {
	if supports != nil {
		if tiers := effortValues(supports["reasoning_effort"]); len(tiers) > 0 {
			return tiers
		}
	}
	for _, key := range []string{
		"reasoning_effort", "reasoning_efforts", "supported_reasoning_efforts",
		"supported_reasoning_levels", "reasoning_levels",
	} {
		if tiers := effortValues(object[key]); len(tiers) > 0 {
			return tiers
		}
	}
	if options, ok := object["reasoning_options"].([]any); ok {
		for _, option := range options {
			item, ok := option.(map[string]any)
			if !ok || !strings.EqualFold(firstString(item, "type"), "effort") {
				continue
			}
			if tiers := effortValues(item["values"]); len(tiers) > 0 {
				return tiers
			}
		}
	}
	return nil
}

// effortValues accepts ["low","high"] or [{"effort":"low"}, ...] (Codex's
// supported_reasoning_levels shape) and returns lowercase, de-duplicated tiers.
func effortValues(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	tiers := make([]string, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	for _, item := range list {
		var tier string
		switch typed := item.(type) {
		case string:
			tier = typed
		case map[string]any:
			tier = firstString(typed, "effort", "value", "id", "name")
		}
		tier = strings.ToLower(strings.TrimSpace(tier))
		if tier == "" {
			continue
		}
		if _, duplicate := seen[tier]; duplicate {
			continue
		}
		seen[tier] = struct{}{}
		tiers = append(tiers, tier)
	}
	if len(tiers) == 0 {
		return nil
	}
	return tiers
}

// capabilitySet is a lowercase set built from list-shaped capability fields
// (LM Studio / Ollama: "capabilities": ["tool_use","vision","thinking"]).
type capabilitySet map[string]struct{}

func capabilityList(object map[string]any) capabilitySet {
	list, ok := stringList(object["capabilities"])
	if !ok {
		return nil
	}
	set := make(capabilitySet, len(list))
	for _, item := range list {
		set[strings.ToLower(strings.TrimSpace(item))] = struct{}{}
	}
	return set
}

func (set capabilitySet) has(names ...string) bool {
	for _, name := range names {
		if _, ok := set[name]; ok {
			return true
		}
	}
	return false
}

func lookup(object map[string]any, path ...string) any {
	var current any = object
	for _, key := range path {
		next, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = next[key]
	}
	return current
}

func nestedMap(object map[string]any, path ...string) map[string]any {
	value, _ := lookup(object, path...).(map[string]any)
	return value
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstBool(object map[string]any, paths ...[]string) (value, ok bool) {
	for _, path := range paths {
		if flag, isBool := lookup(object, path...).(bool); isBool {
			return flag, true
		}
	}
	return false, false
}

func boolPtr(value bool) *bool { return &value }

func stringList(value any) ([]string, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out, true
}

func containsFold(list []string, target string) bool {
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

// number reads a JSON number from any of the shapes encoding/json produces
// (json.Number with UseNumber, float64 without) plus numeric strings, which
// some gateways emit for token limits.
func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	}
	return 0, false
}

func positiveNumber(value any) (float64, bool) {
	parsed, ok := number(value)
	return parsed, ok && parsed > 0
}

func positiveInt(value any) (int, bool) {
	parsed, ok := positiveNumber(value)
	if !ok {
		return 0, false
	}
	return int(parsed), true
}
