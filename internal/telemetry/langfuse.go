package telemetry

import (
	"context"
	"time"

	langfuseacl "github.com/cloudwego/eino-ext/libs/acl/langfuse"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
)

const defaultFlushTimeout = 3 * time.Second

type contextKey string

const traceIDKey contextKey = "langfuse_trace_id"
const parentSpanIDKey contextKey = "langfuse_parent_span_id"

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
func (t *LangfuseTracer) WithNewTrace(ctx context.Context, name string) context.Context {
	traceID, err := t.client.CreateTrace(&langfuseacl.TraceEventBody{
		BaseEventBody: langfuseacl.BaseEventBody{Name: name},
		TimeStamp:     time.Now(),
	})
	if err != nil {
		config.Logger().Printf("[langfuse] CreateTrace error: %v\n", err)
		return ctx
	}
	return context.WithValue(ctx, traceIDKey, traceID)
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

// ChildAgentMiddleware returns an adk.AgentMiddleware for child agents (subagents/teammates).
// It nests generations and tool spans under the parent span stored in context.
func (t *LangfuseTracer) ChildAgentMiddleware() adk.AgentMiddleware {
	return t.buildMiddleware(true)
}

// AgentMiddleware returns an adk.AgentMiddleware that records model generations
// and tool-call spans to Langfuse, keyed by the traceID stored in the context.
func (t *LangfuseTracer) AgentMiddleware() adk.AgentMiddleware {
	return t.buildMiddleware(false)
}

func (t *LangfuseTracer) buildMiddleware(useParentSpan bool) adk.AgentMiddleware {
	return adk.AgentMiddleware{
		BeforeChatModel: func(ctx context.Context, state *adk.ChatModelAgentState) error {
			traceID, _ := ctx.Value(traceIDKey).(string)
			if traceID == "" {
				return nil
			}
			parentObsID := ""
			if useParentSpan {
				parentObsID, _ = ctx.Value(parentSpanIDKey).(string)
			}
			genID, _ := t.client.CreateGeneration(&langfuseacl.GenerationEventBody{
				BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
					BaseEventBody:       langfuseacl.BaseEventBody{Name: "chat_model"},
					TraceID:             traceID,
					ParentObservationID: parentObsID,
					StartTime:           time.Now(),
				},
				InMessages: state.Messages,
			})
			_ = adk.SetRunLocalValue(ctx, "langfuse_gen_id", genID)
			return nil
		},

		AfterChatModel: func(ctx context.Context, state *adk.ChatModelAgentState) error {
			genID := ""
			if val, found, err := adk.GetRunLocalValue(ctx, "langfuse_gen_id"); err == nil && found {
				genID, _ = val.(string)
			}
			if genID == "" {
				return nil
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
			_ = t.client.EndGeneration(&langfuseacl.GenerationEventBody{
				BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
					BaseEventBody: langfuseacl.BaseEventBody{ID: genID},
				},
				OutMessage: outMsg,
				EndTime:    time.Now(),
			})
			return nil
		},

		WrapToolCall: compose.ToolMiddleware{
			Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
				return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
					traceID, _ := ctx.Value(traceIDKey).(string)
					start := time.Now()
					var spanID string
					if traceID != "" {
						parentObsID := ""
						if useParentSpan {
							parentObsID, _ = ctx.Value(parentSpanIDKey).(string)
						}
						spanID, _ = t.client.CreateSpan(&langfuseacl.SpanEventBody{
							BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
								BaseEventBody:       langfuseacl.BaseEventBody{Name: input.Name},
								TraceID:             traceID,
								ParentObservationID: parentObsID,
								Input:               input.Arguments,
								StartTime:           start,
							},
						})
					}
					out, err := next(ctx, input)
					if spanID != "" {
						output := ""
						if out != nil {
							output = out.Result
						}
						_ = t.client.EndSpan(&langfuseacl.SpanEventBody{
							BaseObservationEventBody: langfuseacl.BaseObservationEventBody{
								BaseEventBody: langfuseacl.BaseEventBody{ID: spanID},
								Output:        output,
							},
							EndTime: time.Now(),
						})
					}
					return out, err
				}
			},
		},
	}
}
