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

// toolDisclosureGroups contains only descriptors that remain Deferred after
// transport, mode, and capability gates have been applied. A grouped tool that
// moved to Hidden is therefore neither returned by tool_search nor registered
// as an executable peer.
type toolDisclosureGroups struct {
	byTool  map[string]string
	members map[string][]string
}

func disclosureGroupsFromDescriptors(descriptors []ToolDescriptor) toolDisclosureGroups {
	groups := toolDisclosureGroups{
		byTool:  make(map[string]string),
		members: make(map[string][]string),
	}
	for _, descriptor := range descriptors {
		if descriptor.Exposure != ToolExposureDeferred {
			continue
		}
		group := strings.TrimSpace(descriptor.DisclosureGroup)
		if group == "" {
			continue
		}
		name := strings.TrimSpace(descriptor.Name)
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
