package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	internalmodel "github.com/cnjack/jcode/internal/model"
)

const defaultToolSearchMaxResults = 5

// ToolObservationKind identifies one metadata-only progressive-disclosure
// observation. Observations deliberately exclude model messages, raw search
// queries, tool arguments, schemas, outputs, and errors.
type ToolObservationKind string

const (
	ToolObservationModelRequest ToolObservationKind = "model_request"
	ToolObservationSearch       ToolObservationKind = "tool_search"
	ToolObservationBypass       ToolObservationKind = "deferred_bypass"
)

// ToolObservation is safe-to-persist metadata used by the evaluation harness.
// Only the fields relevant to Kind are populated.
type ToolObservation struct {
	Kind ToolObservationKind

	ModelRequestSeq      int
	VisibleNames         []string
	VisibleCount         int
	SchemaBytes          int
	SchemaTokensEstimate int64
	NewlyVisibleDeferred []string

	ToolCallID           string
	QueryMode            string
	QueryBytes           int
	TermCount            int
	RequiredTermCount    int
	MaxResults           int
	ValidatedSelectNames []string
	UnknownSelectCount   int
	MatchNames           []string
	NewMatchNames        []string
	RepeatedQuery        bool
	Redundant            bool
	Success              bool

	ToolName string
	Reason   string
}

// ToolObservationSink receives metadata synchronously. Implementations must be
// goroutine-safe because tool calls in one model response may run concurrently.
type ToolObservationSink func(ToolObservation)

type toolObservationContextKey struct{}

type toolObservationRun struct {
	mu sync.Mutex

	sink            ToolObservationSink
	modelRequestSeq int
	lastVisible     map[string]bool
	activated       map[string]bool
	queryHashes     map[[sha256.Size]byte]bool
}

// WithToolObservationSink enables metadata collection for one runner turn.
// The shared run object keeps request sequence numbers monotonic across the
// runner's bounded continuation calls.
func WithToolObservationSink(ctx context.Context, sink ToolObservationSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, toolObservationContextKey{}, &toolObservationRun{
		sink:        sink,
		lastVisible: make(map[string]bool),
		activated:   make(map[string]bool),
		queryHashes: make(map[[sha256.Size]byte]bool),
	})
}

type toolObservationMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	deferred map[string]bool
}

func newToolObservationMiddleware(
	ctx context.Context,
	deferred []tool.BaseTool,
) (adk.ChatModelAgentMiddleware, error) {
	names := make(map[string]bool, len(deferred))
	for _, endpoint := range deferred {
		info, err := endpoint.Info(ctx)
		if err != nil {
			return nil, err
		}
		if info != nil {
			names[info.Name] = true
		}
	}
	return &toolObservationMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		deferred:                     names,
	}, nil
}

func observationRunFromContext(ctx context.Context) *toolObservationRun {
	run, _ := ctx.Value(toolObservationContextKey{}).(*toolObservationRun)
	return run
}

func (m *toolObservationMiddleware) WrapModel(
	ctx context.Context,
	endpoint model.BaseChatModel,
	mc *adk.ModelContext,
) (model.BaseChatModel, error) {
	run := observationRunFromContext(ctx)
	if run == nil || mc == nil {
		return endpoint, nil
	}

	// WrapModel is the only Eino hook that observes the final options on every
	// provider attempt (including retries). BeforeModelRewriteState is the right
	// mutation hook, but it cannot provide this per-attempt evidence.
	infos := append([]*schema.ToolInfo(nil), mc.Tools...) //nolint:staticcheck // intentional final-attempt observation
	names := make([]string, 0, len(infos))
	visible := make(map[string]bool, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		names = append(names, info.Name)
		visible[info.Name] = true
	}
	schemaJSON, _ := json.Marshal(infos)

	run.mu.Lock()
	run.modelRequestSeq++
	seq := run.modelRequestSeq
	newlyVisible := make([]string, 0)
	for name := range m.deferred {
		if visible[name] && !run.lastVisible[name] {
			newlyVisible = append(newlyVisible, name)
		}
	}
	run.lastVisible = visible
	sink := run.sink
	run.mu.Unlock()

	sort.Strings(newlyVisible)
	sink(ToolObservation{
		Kind:                 ToolObservationModelRequest,
		ModelRequestSeq:      seq,
		VisibleNames:         names,
		VisibleCount:         len(names),
		SchemaBytes:          len(schemaJSON),
		SchemaTokensEstimate: internalmodel.EstimateTokens(string(schemaJSON)),
		NewlyVisibleDeferred: newlyVisible,
	})
	return endpoint, nil
}

func (m *toolObservationMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tc *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tc == nil {
		return endpoint, nil
	}
	name, callID := tc.Name, tc.CallID
	return func(ctx context.Context, arguments string, opts ...tool.Option) (string, error) {
		m.recordBypass(ctx, name, callID)
		result, err := endpoint(ctx, arguments, opts...)
		if name == ToolSearchReservedName {
			m.recordSearch(ctx, callID, arguments, result, err == nil)
		}
		return result, err
	}, nil
}

func (m *toolObservationMiddleware) WrapEnhancedInvokableToolCall(
	_ context.Context,
	endpoint adk.EnhancedInvokableToolCallEndpoint,
	tc *adk.ToolContext,
) (adk.EnhancedInvokableToolCallEndpoint, error) {
	if tc == nil {
		return endpoint, nil
	}
	name, callID := tc.Name, tc.CallID
	return func(
		ctx context.Context,
		argument *schema.ToolArgument,
		opts ...tool.Option,
	) (*schema.ToolResult, error) {
		m.recordBypass(ctx, name, callID)
		return endpoint(ctx, argument, opts...)
	}, nil
}

func (m *toolObservationMiddleware) recordBypass(ctx context.Context, name, callID string) {
	if !m.deferred[name] {
		return
	}
	run := observationRunFromContext(ctx)
	if run == nil {
		return
	}

	run.mu.Lock()
	if run.lastVisible[name] {
		run.mu.Unlock()
		return
	}
	seq := run.modelRequestSeq
	reason := "not_visible_in_last_model_request"
	if seq == 0 {
		reason = "no_model_request_observed"
	}
	sink := run.sink
	run.mu.Unlock()

	sink(ToolObservation{
		Kind:            ToolObservationBypass,
		ModelRequestSeq: seq,
		ToolCallID:      callID,
		ToolName:        name,
		Reason:          reason,
	})
}

type observedToolSearchArgs struct {
	Query      string `json:"query"`
	MaxResults *int   `json:"max_results,omitempty"`
}

type observedToolSearchResult struct {
	Matches []string `json:"matches"`
}

func (m *toolObservationMiddleware) recordSearch(
	ctx context.Context,
	callID, arguments, output string,
	succeeded bool,
) {
	run := observationRunFromContext(ctx)
	if run == nil {
		return
	}

	observation := ToolObservation{
		Kind:       ToolObservationSearch,
		ToolCallID: callID,
		QueryMode:  "invalid",
		MaxResults: defaultToolSearchMaxResults,
		Success:    succeeded,
	}
	var args observedToolSearchArgs
	parsedArgs := json.Unmarshal([]byte(arguments), &args) == nil
	query := strings.TrimSpace(args.Query)
	if parsedArgs {
		observation.QueryBytes = len(query)
		if args.MaxResults != nil && *args.MaxResults > 0 {
			observation.MaxResults = *args.MaxResults
		}
		observation.QueryMode = "keyword"
		if strings.HasPrefix(query, "select:") {
			observation.QueryMode = "select"
			for _, candidate := range strings.Split(strings.TrimPrefix(query, "select:"), ",") {
				candidate = strings.TrimSpace(candidate)
				if candidate == "" {
					continue
				}
				if m.deferred[candidate] {
					observation.ValidatedSelectNames = append(observation.ValidatedSelectNames, candidate)
				} else {
					observation.UnknownSelectCount++
				}
			}
		} else {
			terms := strings.Fields(query)
			observation.TermCount = len(terms)
			for _, term := range terms {
				if strings.HasPrefix(term, "+") && len(term) > 1 {
					observation.RequiredTermCount++
				}
			}
		}
	}

	var result observedToolSearchResult
	if succeeded && json.Unmarshal([]byte(output), &result) == nil {
		for _, name := range result.Matches {
			if m.deferred[name] {
				observation.MatchNames = append(observation.MatchNames, name)
			}
		}
	} else {
		observation.Success = false
	}

	sort.Strings(observation.ValidatedSelectNames)
	sort.Strings(observation.MatchNames)
	queryHash := sha256.Sum256([]byte(query + "\x00" + observation.QueryMode))

	run.mu.Lock()
	observation.ModelRequestSeq = run.modelRequestSeq
	if parsedArgs {
		observation.RepeatedQuery = run.queryHashes[queryHash]
		run.queryHashes[queryHash] = true
	}
	for _, name := range observation.MatchNames {
		if !run.activated[name] {
			observation.NewMatchNames = append(observation.NewMatchNames, name)
			run.activated[name] = true
		}
	}
	observation.Redundant = observation.Success && len(observation.NewMatchNames) == 0
	sink := run.sink
	run.mu.Unlock()

	sink(observation)
}
