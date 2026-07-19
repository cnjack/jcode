package agent

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

// toolDisclosureGroups tracks the canonical names that remain Deferred after
// transport, mode, and capability gates, plus their optional narrow groups. A
// tool moved to Hidden is therefore neither a compatibility rewrite candidate,
// nor returned by group expansion, nor registered as an executable peer.
type toolDisclosureGroups struct {
	deferred map[string]bool
	byTool   map[string]string
	members  map[string][]string
}

func disclosureGroupsFromDescriptors(descriptors []ToolDescriptor) toolDisclosureGroups {
	groups := toolDisclosureGroups{
		deferred: make(map[string]bool),
		byTool:   make(map[string]string),
		members:  make(map[string][]string),
	}
	for _, descriptor := range descriptors {
		if descriptor.Exposure != ToolExposureDeferred {
			continue
		}
		name := strings.TrimSpace(descriptor.Name)
		groups.deferred[name] = true
		group := strings.TrimSpace(descriptor.DisclosureGroup)
		if group == "" {
			continue
		}
		groups.byTool[name] = group
		groups.members[group] = append(groups.members[group], name)
	}
	for group := range groups.members {
		sort.Strings(groups.members[group])
	}
	return groups
}

func (g toolDisclosureGroups) useful() bool {
	for _, members := range g.members {
		if len(members) > 1 {
			return true
		}
	}
	return false
}

// toolSearchDisclosureMiddleware expands successful client-side tool_search
// observations before they are written to conversation history. Eino's
// forward-selection pass then treats the explicit peers exactly like ordinary
// matches on the next model generation; same-batch activation remains
// deliberately unsupported.
type toolSearchDisclosureMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	groups toolDisclosureGroups
}

func newToolSearchDisclosureMiddleware(groups toolDisclosureGroups) adk.ChatModelAgentMiddleware {
	if !groups.useful() {
		return nil
	}
	return &toolSearchDisclosureMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		groups:                       groups,
	}
}

func (m *toolSearchDisclosureMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tc *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tc == nil || tc.Name != ToolSearchReservedName {
		return endpoint, nil
	}
	return func(ctx context.Context, arguments string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, arguments, opts...)
		if err != nil {
			return result, err
		}
		expanded, ok := expandToolSearchResult(result, m.groups)
		if !ok {
			return result, nil
		}
		return expanded, nil
	}, nil
}

const (
	minToolSearchExactListNames = 2
	// Eight covers the largest built-in Browser/Computer capability family
	// while keeping malformed catalog-wide comma lists fail closed.
	maxToolSearchExactListNames = 8
	// One guessed identifier is tolerated only when the model also supplied at
	// least two exact effective names. It is dropped rather than treated as an
	// alias, matching Eino select's result without widening the catalog.
	maxToolSearchExactListUnknownNames = 1
)

// toolSearchExactListMiddleware handles a narrow client ToolSearch formatting
// error seen from some models: they emit several exact Deferred names separated
// by commas, but omit Eino's required "select:" prefix. It is intentionally
// placed inside observation, caller handlers, and PreToolUse so those layers
// retain the model-issued arguments. Approval and the real ToolSearch endpoint
// both receive the repaired query; tool_search is a read-only operation, so the
// normalization does not widen authorization.
type toolSearchExactListMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	deferred map[string]bool
}

func newToolSearchExactListMiddleware(deferred map[string]bool) adk.ChatModelAgentMiddleware {
	if len(deferred) < minToolSearchExactListNames {
		return nil
	}
	canonical := make(map[string]bool, len(deferred))
	for name, enabled := range deferred {
		if enabled {
			canonical[name] = true
		}
	}
	if len(canonical) < minToolSearchExactListNames {
		return nil
	}
	return &toolSearchExactListMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		deferred:                     canonical,
	}
}

func (m *toolSearchExactListMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tc *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tc == nil || tc.Name != ToolSearchReservedName {
		return endpoint, nil
	}
	return func(ctx context.Context, arguments string, opts ...tool.Option) (string, error) {
		if rewritten, ok := rewriteToolSearchExactNameList(arguments, m.deferred); ok {
			arguments = rewritten
		}
		return endpoint(ctx, arguments, opts...)
	}, nil
}

// rewriteToolSearchExactNameList returns ok only for an unambiguous list of two
// through eight distinct identifiers containing at least two canonical names in
// the current effective Deferred catalog. At most one syntactically tool-like
// unknown may be present; it is discarded, never mapped to an alias. This mirrors
// Eino's explicit select semantics (unknown names are ignored) while feeding only
// exact effective names from the model-issued list into select. Any later group
// expansion remains plan-gated, and target approval is unchanged.
//
// The max_results field is retained byte-for-byte, but Eino deliberately ignores
// it in direct-selection mode; the eight-name compatibility ceiling is therefore
// the hard disclosure bound here. Unknown JSON fields, duplicate fields, malformed
// JSON, invalid max_results values, semantic queries, unsafe unknown identifiers,
// and out-of-range lists are passed to Eino unchanged (fail closed).
func rewriteToolSearchExactNameList(arguments string, deferred map[string]bool) (rewritten string, ok bool) {
	query, start, end, parsed := parseToolSearchExactListEnvelope(arguments)
	if !parsed || strings.HasPrefix(strings.TrimSpace(query), "select:") {
		return "", false
	}

	parts := strings.Split(query, ",")
	if len(parts) < minToolSearchExactListNames || len(parts) > maxToolSearchExactListNames {
		return "", false
	}
	names := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	unknownCount := 0
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] {
			return "", false
		}
		seen[name] = true
		if deferred[name] {
			names = append(names, name)
			continue
		}
		if !isToolSearchUnknownIdentifier(name) {
			return "", false
		}
		unknownCount++
		if unknownCount > maxToolSearchExactListUnknownNames {
			return "", false
		}
	}
	if len(names) < minToolSearchExactListNames {
		return "", false
	}

	encoded, err := json.Marshal("select:" + strings.Join(names, ","))
	if err != nil {
		return "", false
	}
	return arguments[:start] + string(encoded) + arguments[end:], true
}

func isToolSearchUnknownIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isToolNameByte(name[i]) {
			return false
		}
	}
	return true
}

func parseToolSearchExactListEnvelope(arguments string) (query string, start, end int, ok bool) {
	decoder := json.NewDecoder(strings.NewReader(arguments))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return "", 0, 0, false
	}

	seen := make(map[string]bool, 2)
	queryFound := false
	for decoder.More() {
		token, err = decoder.Token()
		key, keyOK := token.(string)
		if err != nil || !keyOK || seen[key] {
			return "", 0, 0, false
		}
		seen[key] = true

		valuePrefix := int(decoder.InputOffset())
		var raw json.RawMessage
		if err = decoder.Decode(&raw); err != nil {
			return "", 0, 0, false
		}
		valueEnd := int(decoder.InputOffset())
		switch key {
		case "query":
			if err = json.Unmarshal(raw, &query); err != nil {
				return "", 0, 0, false
			}
			relativeStart := strings.LastIndex(arguments[valuePrefix:valueEnd], string(raw))
			if relativeStart < 0 {
				return "", 0, 0, false
			}
			start = valuePrefix + relativeStart
			end = start + len(raw)
			queryFound = true
		case "max_results":
			var maxResults *int
			if err = json.Unmarshal(raw, &maxResults); err != nil {
				return "", 0, 0, false
			}
		default:
			return "", 0, 0, false
		}
	}

	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || !queryFound || !atJSONEOF(decoder) {
		return "", 0, 0, false
	}
	return query, start, end, true
}

type toolSearchDisclosureResult struct {
	Matches []string `json:"matches"`
}

// expandToolSearchResult returns ok only when at least one effective grouped
// Deferred match caused a change. Unknown shapes, malformed results, and
// ungrouped matches are returned byte-for-byte by the caller (fail closed).
func expandToolSearchResult(output string, groups toolDisclosureGroups) (expanded string, ok bool) {
	matches, parsed := parseToolSearchMatches(output)
	if !parsed {
		return "", false
	}

	seenNames := make(map[string]bool, len(matches))
	stableMatches := make([]string, 0, len(matches))
	hitGroups := make([]string, 0)
	seenGroups := make(map[string]bool)
	for _, name := range matches {
		if !seenNames[name] {
			stableMatches = append(stableMatches, name)
			seenNames[name] = true
		}
		group := groups.byTool[name]
		if group != "" && !seenGroups[group] {
			hitGroups = append(hitGroups, group)
			seenGroups[group] = true
		}
	}
	if len(hitGroups) == 0 {
		return "", false
	}

	for _, group := range hitGroups {
		for _, peer := range groups.members[group] {
			if seenNames[peer] {
				continue
			}
			stableMatches = append(stableMatches, peer)
			seenNames[peer] = true
		}
	}
	if len(stableMatches) == len(matches) {
		identical := true
		for i := range matches {
			if stableMatches[i] != matches[i] {
				identical = false
				break
			}
		}
		if identical {
			return "", false
		}
	}

	encoded, err := json.Marshal(toolSearchDisclosureResult{Matches: stableMatches})
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func parseToolSearchMatches(output string) ([]string, bool) {
	var envelope map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(output))
	if err := decoder.Decode(&envelope); err != nil || len(envelope) != 1 {
		return nil, false
	}
	if !atJSONEOF(decoder) {
		return nil, false
	}
	raw, ok := envelope["matches"]
	if !ok {
		return nil, false
	}
	var matches []string
	if err := json.Unmarshal(raw, &matches); err != nil {
		return nil, false
	}
	return matches, true
}

func atJSONEOF(decoder *json.Decoder) bool {
	var trailing any
	return decoder.Decode(&trailing) == io.EOF
}
