package telemetry

import (
	"context"
	"slices"
	"strings"
	"time"

	langfuseacl "github.com/cloudwego/eino-ext/libs/acl/langfuse"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
)

const defaultFlushTimeout = 3 * time.Second

const telemetryImagePlaceholder = "[image omitted from telemetry]"

type contextKey string

const traceIDKey contextKey = "langfuse_trace_id"
const parentSpanIDKey contextKey = "langfuse_parent_span_id"
const toolSpanTracerKey contextKey = "langfuse_tool_span_tracer"

// SubSpanFunc creates a child span under the current tool span.
// It returns a finish function that must be called with the output string.
type SubSpanFunc func(name string) (finish func(output string))

// SubSpanFromContext retrieves the sub-span creator stored by the langfuse
// WrapToolCall middleware. Returns nil if tracing is not active.
func SubSpanFromContext(ctx context.Context) SubSpanFunc {
	fn, _ := ctx.Value(toolSpanTracerKey).(SubSpanFunc)
	return fn
}

// LangfuseTracer wraps the Langfuse client and provides eino integration helpers.
type LangfuseTracer struct {
	client langfuseacl.Langfuse
}

// NewLangfuseTracer creates a LangfuseTracer from the config.
// Returns nil if required credentials are missing.
func NewLangfuseTracer(cfg *config.LangfuseConfig) *LangfuseTracer {
	if cfg == nil || cfg.SecretKey == "" || cfg.PublicKey == "" {
		return nil
	}
	host := cfg.Host
	if host == "" {
		host = "https://cloud.langfuse.com"
	}
	return &LangfuseTracer{
		client: langfuseacl.NewLangfuse(host, cfg.PublicKey, cfg.SecretKey),
	}
}

// WithNewTrace creates a new Langfuse trace and returns a context carrying its ID.
// The trace records the latest user message so the top-level Langfuse input
// represents the user turn rather than only its child generation inputs.
func (t *LangfuseTracer) WithNewTrace(ctx context.Context, name string, messages []*schema.Message) context.Context {
	traceID, err := t.client.CreateTrace(&langfuseacl.TraceEventBody{
		BaseEventBody: langfuseacl.BaseEventBody{Name: name},
		TimeStamp:     time.Now(),
		Input:         traceInput(messages),
	})
	if err != nil {
		config.Logger().Printf("[langfuse] CreateTrace error: %v\n", err)
		return ctx
	}
	return context.WithValue(ctx, traceIDKey, traceID)
}

// EndTrace records the final response on the current Langfuse trace.
func (t *LangfuseTracer) EndTrace(ctx context.Context, output string) {
	traceID, _ := ctx.Value(traceIDKey).(string)
	if traceID == "" {
		return
	}
	if err := t.client.EndTrace(&langfuseacl.TraceEventBody{
		BaseEventBody: langfuseacl.BaseEventBody{
			ID: traceID,
		},
		TimeStamp: time.Now(),
		Output:    output,
	}); err != nil {
		config.Logger().Printf("[langfuse] EndTrace error: %v\n", err)
	}
}

// traceInput returns the latest user message in a form safe to send to
// Langfuse. Multimodal payloads retain their text but replace images with a
// marker, preventing image bytes or URLs from becoming trace input.
func traceInput(messages []*schema.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg == nil || msg.Role != schema.User {
			continue
		}
		if len(msg.UserInputMultiContent) == 0 {
			return msg.Content
		}

		parts := make([]string, 0, len(msg.UserInputMultiContent))
		for _, part := range msg.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeText {
				if part.Text != "" {
					parts = append(parts, part.Text)
				}
				continue
			}
			parts = append(parts, telemetryImagePlaceholder)
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// Flush ensures all buffered events are sent to Langfuse.
// It blocks at most defaultFlushTimeout to avoid stalling program exit.
func (t *LangfuseTracer) Flush() {
	done := make(chan struct{})
	go func() {
		t.client.Flush()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(defaultFlushTimeout):
		config.Logger().Println("[langfuse] flush timed out")
	}
}

// WithChildTrace creates a child span under the current trace and returns a context
// carrying both the original traceID and the new parentSpanID. This allows subagent
// and teammate agent calls to appear as nested spans in Langfuse.
func (t *LangfuseTracer) WithChildTrace(ctx context.Context, name string) context.Context {
	traceID, _ := ctx.Value(traceIDKey).(string)
	if traceID == "" {
		return ctx
	}
	parentSpanID, _ := t.client.CreateSpan(&langfuseacl.SpanEventBody{
		BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
			BaseEventBody: langfuseacl.BaseEventBody{Name: name},
			TraceID:       traceID,
			StartTime:     time.Now(),
		},
	})
	if parentSpanID == "" {
		return ctx
	}
	return context.WithValue(ctx, parentSpanIDKey, parentSpanID)
}

// EndChildTrace closes the child span created by WithChildTrace.
func (t *LangfuseTracer) EndChildTrace(ctx context.Context, output string) {
	spanID, _ := ctx.Value(parentSpanIDKey).(string)
	if spanID == "" {
		return
	}
	_ = t.client.EndSpan(&langfuseacl.SpanEventBody{
		BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
			BaseEventBody: langfuseacl.BaseEventBody{ID: spanID},
			Output:        output,
		},
		EndTime: time.Now(),
	})
}

// ChildAgentMiddleware returns a ChatModelAgentMiddleware for child agents
// (subagents/teammates). It nests generations and tool spans under the parent
// span stored in context.
func (t *LangfuseTracer) ChildAgentMiddleware() adk.ChatModelAgentMiddleware {
	return &langfuseMiddleware{tracer: t, useParentSpan: true}
}

// AgentMiddleware returns a ChatModelAgentMiddleware that records model
// generations and tool-call spans to Langfuse, keyed by the traceID stored in
// the context.
func (t *LangfuseTracer) AgentMiddleware() adk.ChatModelAgentMiddleware {
	return &langfuseMiddleware{tracer: t, useParentSpan: false}
}

// langfuseMiddleware implements adk.ChatModelAgentMiddleware. It embeds the adk
// base handler so only the hooks it needs are overridden (model generation
// spans + tool-call spans); every other interface method is a no-op default.
type langfuseMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	tracer        *LangfuseTracer
	useParentSpan bool
}

// BeforeModelRewriteState opens a Langfuse generation span for the model call.
func (mw *langfuseMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	t := mw.tracer
	traceID, _ := ctx.Value(traceIDKey).(string)
	if traceID == "" {
		return ctx, state, nil
	}
	parentObsID := ""
	if mw.useParentSpan {
		parentObsID, _ = ctx.Value(parentSpanIDKey).(string)
	}
	genID, _ := t.client.CreateGeneration(&langfuseacl.GenerationEventBody{
		BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
			BaseEventBody:       langfuseacl.BaseEventBody{Name: "chat_model"},
			TraceID:             traceID,
			ParentObservationID: parentObsID,
			StartTime:           time.Now(),
		},
		// Generation input is shipped to the configured Langfuse host. Enhanced
		// tool results keep screenshots in UserInputMultiContent, and the
		// langfuse Eino adapter does not recognize that newer field as media: it
		// would serialize Base64Data verbatim into the trace event. Build a
		// detached, text-safe view instead. The live state remains unchanged and
		// still carries the pixels to the model.
		InMessages: traceSafeMessages(state.Messages),
	})
	_ = adk.SetRunLocalValue(ctx, "langfuse_gen_id", genID)
	return ctx, state, nil
}

// traceSafeMessages returns a detached view of messages for an external trace
// sink. UserInputMultiContent images are replaced in place-order with a plain
// text marker; neither Base64Data nor a remote/local URL crosses the telemetry
// boundary. The image-bearing part is deliberately never copied, which avoids
// making a second large in-memory copy just to redact it.
func traceSafeMessages(messages []*schema.Message) []*schema.Message {
	if messages == nil {
		return nil
	}
	out := make([]*schema.Message, len(messages))
	for i, msg := range messages {
		if msg == nil {
			continue
		}
		clone := *msg
		clone.MultiContent = slices.Clone(msg.MultiContent)
		clone.AssistantGenMultiContent = append([]schema.MessageOutputPart(nil), msg.AssistantGenMultiContent...)
		clone.ToolCalls = append([]schema.ToolCall(nil), msg.ToolCalls...)
		if len(msg.UserInputMultiContent) > 0 {
			clone.UserInputMultiContent = make([]schema.MessageInputPart, 0, len(msg.UserInputMultiContent))
			for _, part := range msg.UserInputMultiContent {
				if part.Type == schema.ChatMessagePartTypeImageURL {
					clone.UserInputMultiContent = append(clone.UserInputMultiContent, schema.MessageInputPart{
						Type: schema.ChatMessagePartTypeText,
						Text: telemetryImagePlaceholder,
					})
					continue
				}
				clone.UserInputMultiContent = append(clone.UserInputMultiContent, part)
			}
		}
		out[i] = &clone
	}
	return out
}

// AfterModelRewriteState closes the generation span and records token usage.
func (mw *langfuseMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	t := mw.tracer
	genID := ""
	if val, found, err := adk.GetRunLocalValue(ctx, "langfuse_gen_id"); err == nil && found {
		genID, _ = val.(string)
	}
	if genID == "" {
		return ctx, state, nil
	}
	_ = adk.DeleteRunLocalValue(ctx, "langfuse_gen_id")
	// Find the last assistant message to record as output.
	var outMsg *schema.Message
	for i := len(state.Messages) - 1; i >= 0; i-- {
		if state.Messages[i].Role == schema.Assistant {
			outMsg = state.Messages[i]
			break
		}
	}

	// Read per-call token usage from the context-local TokenUsage tracker.
	// Langfuse bills and charts from usageDetails + model; cached/reasoning
	// belong in the nested details objects, not string metadata.
	var usage *langfuseacl.Usage
	var modelName string
	if tracker := internalmodel.TokenTrackerFromContext(ctx); tracker != nil {
		usage = langfuseUsage(tracker.GetLastDetail())
		modelName = tracker.GetLastModel()
	}

	_ = t.client.EndGeneration(&langfuseacl.GenerationEventBody{
		BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
			BaseEventBody: langfuseacl.BaseEventBody{ID: genID},
		},
		OutMessage: outMsg,
		EndTime:    time.Now(),
		Model:      modelName,
		Usage:      usage,
	})
	return ctx, state, nil
}

// langfuseUsage maps one API call onto Langfuse's OpenAI-shaped usageDetails,
// including cached_tokens / reasoning_tokens so the host can cost them.
func langfuseUsage(d *internalmodel.TokenUsageDetail) *langfuseacl.Usage {
	if d == nil || (d.TotalTokens <= 0 && d.PromptTokens <= 0 && d.CompletionTokens <= 0) {
		return nil
	}
	return &langfuseacl.Usage{
		PromptTokens:     d.PromptTokens,
		CompletionTokens: d.CompletionTokens,
		TotalTokens:      d.TotalTokens,
		PromptTokensDetails: &langfuseacl.PromptTokensDetails{
			CachedTokens: d.CachedTokens,
		},
		CompletionTokensDetails: &langfuseacl.CompletionTokensDetails{
			ReasoningTokens: d.ReasoningTokens,
		},
	}
}

// WrapInvokableToolCall wraps a tool's execution in a Langfuse span and exposes
// a sub-span creator in context for downstream middleware (e.g. approval).
func (mw *langfuseMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	t := mw.tracer
	toolName := ""
	if tCtx != nil {
		toolName = tCtx.Name
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		traceID, _ := ctx.Value(traceIDKey).(string)
		start := time.Now()
		var spanID string
		if traceID != "" {
			parentObsID := ""
			if mw.useParentSpan {
				parentObsID, _ = ctx.Value(parentSpanIDKey).(string)
			}
			spanID, _ = t.client.CreateSpan(&langfuseacl.SpanEventBody{
				BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
					BaseEventBody:       langfuseacl.BaseEventBody{Name: toolName},
					TraceID:             traceID,
					ParentObservationID: parentObsID,
					Input:               argumentsInJSON,
					StartTime:           start,
				},
			})
		}

		// Store a sub-span creator in context so downstream middleware
		// (e.g. approval) can create child spans under this tool span.
		if spanID != "" {
			subSpanFunc := SubSpanFunc(func(name string) func(output string) {
				childStart := time.Now()
				childID, _ := t.client.CreateSpan(&langfuseacl.SpanEventBody{
					BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
						BaseEventBody:       langfuseacl.BaseEventBody{Name: name},
						TraceID:             traceID,
						ParentObservationID: spanID,
						StartTime:           childStart,
					},
				})
				return func(output string) {
					if childID != "" {
						_ = t.client.EndSpan(&langfuseacl.SpanEventBody{
							BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
								BaseEventBody: langfuseacl.BaseEventBody{ID: childID},
								Output:        output,
							},
							EndTime: time.Now(),
						})
					}
				}
			})
			ctx = context.WithValue(ctx, toolSpanTracerKey, subSpanFunc)
		}

		out, err := endpoint(ctx, argumentsInJSON, opts...)
		if spanID != "" {
			_ = t.client.EndSpan(&langfuseacl.SpanEventBody{
				BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
					BaseEventBody: langfuseacl.BaseEventBody{ID: spanID},
					Output:        out,
				},
				EndTime: time.Now(),
			})
		}
		return out, err
	}, nil
}

// WrapEnhancedInvokableToolCall is the multimodal counterpart of
// WrapInvokableToolCall. Only text parts are sent to Langfuse: screenshots may
// contain sensitive pixels and Base64 blobs are both unsafe and unhelpful in a
// trace. The actual model request still receives every image part.
func (mw *langfuseMiddleware) WrapEnhancedInvokableToolCall(
	_ context.Context,
	endpoint adk.EnhancedInvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.EnhancedInvokableToolCallEndpoint, error) {
	t := mw.tracer
	toolName := ""
	if tCtx != nil {
		toolName = tCtx.Name
	}
	return func(ctx context.Context, argument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
		argumentsInJSON := ""
		if argument != nil {
			argumentsInJSON = argument.Text
		}
		traceID, _ := ctx.Value(traceIDKey).(string)
		start := time.Now()
		var spanID string
		if traceID != "" {
			parentObsID := ""
			if mw.useParentSpan {
				parentObsID, _ = ctx.Value(parentSpanIDKey).(string)
			}
			spanID, _ = t.client.CreateSpan(&langfuseacl.SpanEventBody{
				BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
					BaseEventBody:       langfuseacl.BaseEventBody{Name: toolName},
					TraceID:             traceID,
					ParentObservationID: parentObsID,
					Input:               argumentsInJSON,
					StartTime:           start,
				},
			})
		}
		if spanID != "" {
			subSpanFunc := SubSpanFunc(func(name string) func(output string) {
				childStart := time.Now()
				childID, _ := t.client.CreateSpan(&langfuseacl.SpanEventBody{
					BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
						BaseEventBody:       langfuseacl.BaseEventBody{Name: name},
						TraceID:             traceID,
						ParentObservationID: spanID,
						StartTime:           childStart,
					},
				})
				return func(output string) {
					if childID != "" {
						_ = t.client.EndSpan(&langfuseacl.SpanEventBody{
							BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
								BaseEventBody: langfuseacl.BaseEventBody{ID: childID},
								Output:        output,
							},
							EndTime: time.Now(),
						})
					}
				}
			})
			ctx = context.WithValue(ctx, toolSpanTracerKey, subSpanFunc)
		}

		out, err := endpoint(ctx, argument, opts...)
		if spanID != "" {
			_ = t.client.EndSpan(&langfuseacl.SpanEventBody{
				BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
					BaseEventBody: langfuseacl.BaseEventBody{ID: spanID},
					Output:        enhancedResultText(out),
				},
				EndTime: time.Now(),
			})
		}
		return out, err
	}, nil
}

func enhancedResultText(result *schema.ToolResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range result.Parts {
		if part.Type == schema.ToolPartTypeText {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}
