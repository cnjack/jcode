package agent

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const (
	loadSkillToolName             = "load_skill"
	toolSearchSkillReminderMarker = "jcode_toolsearch_skill_reminder"
)

type toolSearchSkillReminderMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	deferred map[string]bool
}

func newToolSearchSkillReminderMiddleware(
	ctx context.Context,
	deferred []tool.BaseTool,
) (adk.ChatModelAgentMiddleware, error) {
	if len(deferred) == 0 {
		return nil, nil
	}
	names := make(map[string]bool, len(deferred))
	for _, endpoint := range deferred {
		info, err := endpoint.Info(ctx)
		if err != nil {
			return nil, err
		}
		if info != nil && strings.TrimSpace(info.Name) != "" {
			names[info.Name] = true
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	return &toolSearchSkillReminderMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		deferred:                     names,
	}, nil
}

func (m *toolSearchSkillReminderMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	_ *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	if state == nil {
		return ctx, state, nil
	}

	visible := make(map[string]bool, len(state.ToolInfos))
	for _, info := range state.ToolInfos {
		if info != nil {
			visible[info.Name] = true
		}
	}
	if !visible[ToolSearchReservedName] {
		return ctx, state, nil
	}

	matched := make([]*schema.Message, 0)
	for _, loaded := range trailingToolResults(state.Messages) {
		if loaded.ToolName != loadSkillToolName || loaded.ToolCallID == "" ||
			loaded.Content == "" || hasToolSearchSkillReminder(loaded) {
			continue
		}
		for name := range m.deferred {
			if !visible[name] && containsExactToolName(loaded.Content, name) {
				matched = append(matched, loaded)
				break
			}
		}
	}
	if len(matched) == 0 {
		return ctx, state, nil
	}
	for _, loaded := range matched {
		markToolSearchSkillReminder(loaded)
	}
	last := matched[len(matched)-1]
	last.Content += "\n\n<tool-routing-reminder>\n" +
		"The loaded skill names one or more purpose-built tools whose schemas are still deferred. Use tool_search in a separate tool-call batch before substituting execute or another generic tool. " +
		"If a needed schema is now attached, use it directly and do not search for it again; if search misses, report it unavailable instead of using a shell workaround.\n" +
		"</tool-routing-reminder>"
	return ctx, state, nil
}

func markToolSearchSkillReminder(loaded *schema.Message) {
	extra := make(map[string]any, len(loaded.Extra)+1)
	for key, value := range loaded.Extra {
		extra[key] = value
	}
	extra[toolSearchSkillReminderMarker] = loaded.ToolCallID
	loaded.Extra = extra
}

func trailingToolResults(messages []*schema.Message) []*schema.Message {
	results := make([]*schema.Message, 0)
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message == nil {
			continue
		}
		switch message.Role {
		case schema.Tool:
			results = append(results, message)
		case schema.User, schema.Assistant:
			for left, right := 0, len(results)-1; left < right; left, right = left+1, right-1 {
				results[left], results[right] = results[right], results[left]
			}
			return results
		}
	}
	for left, right := 0, len(results)-1; left < right; left, right = left+1, right-1 {
		results[left], results[right] = results[right], results[left]
	}
	return results
}

func hasToolSearchSkillReminder(message *schema.Message) bool {
	if message == nil || message.Extra == nil {
		return false
	}
	source, _ := message.Extra[toolSearchSkillReminderMarker].(string)
	return source != "" && source == message.ToolCallID
}

func containsExactToolName(content, name string) bool {
	if name == "" {
		return false
	}
	for offset := 0; offset <= len(content)-len(name); {
		relative := strings.Index(content[offset:], name)
		if relative < 0 {
			return false
		}
		start := offset + relative
		end := start + len(name)
		beforeOK := start == 0 || !isToolNameByte(content[start-1])
		afterOK := end == len(content) || !isToolNameByte(content[end])
		if beforeOK && afterOK {
			return true
		}
		offset = start + 1
	}
	return false
}

func isToolNameByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_'
}
