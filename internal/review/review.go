// Package review implements jcode's optional LLM approval reviewer: a
// background "guardian" that adjudicates tool calls which would otherwise
// interrupt the user with an approval prompt. It runs a small, dedicated model
// against a risk policy and returns allow / deny / escalate.
//
// The reviewer is opt-in (config approval_review.enabled). When it is not
// configured, ApprovalState never calls it and behavior is identical to before.
//
// Design layers (see internal-doc/approval-review-design.md):
//   - V1: one non-streaming Generate call, strict-JSON verdict, fail-open to the
//     user on any error/timeout, per-turn denial circuit breaker.
//   - V2: optional read-only investigation (the reviewer may run read/grep/glob
//     to gather evidence before deciding).
//   - V3: a reused reviewer conversation so the large policy prefix is served
//     from the provider's prompt cache, kept fully separate from the main
//     conversation's cache.
package review

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
)

// Outcome is the reviewer's verdict for one tool call.
type Outcome int

const (
	// Escalate means the reviewer could not (or should not) decide — the caller
	// falls back to prompting the user. This is the safe default on any failure.
	Escalate Outcome = iota
	// Allow means the action may run without a user prompt.
	Allow
	// Deny means the action must not run; the rationale is surfaced to the agent.
	Deny
)

// Msg is one entry of conversation context handed to the reviewer as evidence.
type Msg struct {
	Role    string // "user" | "assistant" | "tool"
	Content string
}

// Request is a single tool call to adjudicate.
type Request struct {
	ToolName   string
	ToolArgs   string
	Cwd        string
	IsExternal bool  // touches a path outside the workspace
	Transcript []Msg // recent conversation tail (untrusted evidence)
}

// Result is the reviewer's decision.
type Result struct {
	Outcome   Outcome
	RiskLevel string
	Rationale string
	// Failed is true when the review could not complete (model error, timeout,
	// unparseable output). Outcome is then always Escalate. Callers use this to
	// distinguish "reviewer chose to escalate" from "reviewer broke".
	Failed bool
}

// Reviewer adjudicates tool calls. ApprovalState holds one (or nil).
type Reviewer interface {
	Review(ctx context.Context, req Request) Result
}

// Options configure a reviewer Engine.
type Options struct {
	Config      *config.Config
	ModelRef    string // "provider/model" or "small"; "" resolves to small→main
	Policy      string // extra workspace policy text appended to the base policy
	Timeout     time.Duration
	AuditPath   string // JSONL verdict log; "" → <config dir>/approval-review.jsonl
	Investigate bool   // V2: allow read-only tool use during review
	ReuseCache  bool   // V3: reuse a cached reviewer conversation
	Platform    string // V2: platform string for the read-only Env
}

const (
	defaultTimeout = 60 * time.Second
	// transcript / argument caps keep the reviewer prompt bounded and cheap.
	maxTranscriptMsgs = 24
	maxMsgChars       = 2000
	maxArgsChars      = 8000
	// parseAttempts is how many Generate calls we make to coax valid JSON out of
	// a model that wrapped it in prose on the first try.
	parseAttempts = 2
)

// Engine is the concrete Reviewer.
type Engine struct {
	cfg          *config.Config
	factory      *internalmodel.ModelFactory
	modelOverride string
	system       string // full system prompt (policy + contract)
	policyExtra  string
	timeout      time.Duration
	audit        *auditLog
	investigate  bool
	reuseCache   bool
	platform     string
	trunk        *reviewerSession // V3; nil when reuseCache is false
}

// New builds a reviewer Engine from Options. It never returns nil; a
// misconfigured model surfaces per-review as an Escalate (fall back to user).
func New(opts Options) *Engine {
	to := opts.Timeout
	if to <= 0 {
		to = defaultTimeout
	}
	auditPath := opts.AuditPath
	if auditPath == "" {
		auditPath = filepath.Join(config.ConfigDir(), "approval-review.jsonl")
	}
	e := &Engine{
		cfg:           opts.Config,
		factory:       internalmodel.NewModelFactory(opts.Config, nil),
		modelOverride: opts.ModelRef,
		system:        buildSystemPrompt(opts.Policy),
		policyExtra:   opts.Policy,
		timeout:       to,
		audit:         newAuditLog(auditPath),
		investigate:   opts.Investigate,
		reuseCache:    opts.ReuseCache,
		platform:      opts.Platform,
	}
	if opts.ReuseCache {
		e.trunk = newReviewerSession()
	}
	return e
}

// resolveModelRef picks the concrete "provider/model" for the reviewer:
// explicit override → configured small_model → main model. The small alias
// degrades to the main model so an unset small_model still gets a working
// reviewer rather than silently disabling it.
func (e *Engine) resolveModelRef() string {
	ref := strings.TrimSpace(e.modelOverride)
	if ref != "" && ref != internalmodel.SmallModelAlias {
		return ref
	}
	if e.cfg != nil && strings.TrimSpace(e.cfg.SmallModel) != "" {
		return e.cfg.SmallModel
	}
	if e.cfg != nil {
		return e.cfg.Model
	}
	return ""
}

// Review adjudicates one tool call and records the verdict to the audit log.
func (e *Engine) Review(ctx context.Context, req Request) Result {
	start := time.Now()
	res, meta := e.review(ctx, req)
	e.audit.write(auditRecord{
		Tool:         req.ToolName,
		Args:         req.ToolArgs,
		Cwd:          req.Cwd,
		IsExternal:   req.IsExternal,
		Decision:     res.Outcome.String(),
		Risk:         res.RiskLevel,
		UserAuth:     meta.userAuth,
		Rationale:    res.Rationale,
		Failed:       res.Failed,
		FailReason:   meta.failReason,
		Model:        meta.model,
		LatencyMS:    time.Since(start).Milliseconds(),
		PromptTokens: meta.promptTokens,
		CachedTokens: meta.cachedTokens,
		CacheSeen:    meta.cacheSeen,
		Investigated: meta.investigated,
		ReviewCalls:  meta.calls,
	})
	return res
}

// reviewMeta carries per-review telemetry for the audit log.
type reviewMeta struct {
	model        string
	userAuth     string
	failReason   string
	promptTokens int64
	cachedTokens int64
	cacheSeen    bool
	investigated bool
	calls        int64
}

func (e *Engine) review(ctx context.Context, req Request) (Result, reviewMeta) {
	meta := reviewMeta{}
	modelRef := e.resolveModelRef()
	meta.model = internalmodel.BareModelID(modelRef)
	if modelRef == "" {
		meta.failReason = "no model configured"
		return Result{Outcome: Escalate, Failed: true}, meta
	}

	cctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cm, err := e.factory.GetModel(cctx, modelRef)
	if err != nil || cm == nil {
		config.Logger().Printf("[review] model init failed for %q: %v", modelRef, err)
		meta.failReason = "model init failed"
		return Result{Outcome: Escalate, Failed: true}, meta
	}

	tracker := &internalmodel.TokenUsage{}
	cctx = internalmodel.WithTokenTracker(cctx, tracker)

	// Investigation (V2) takes precedence: a read-only tool loop can't share the
	// cached single-shot trunk, so the two features are mutually exclusive per
	// review.
	if e.investigate {
		res, imeta := e.reviewWithTools(cctx, req, cm)
		e.fillTokenMeta(&imeta, tracker)
		imeta.model = meta.model
		imeta.investigated = true
		return res, imeta
	}

	// Reused-session path (V3): adjudicate against a cached reviewer conversation
	// so the policy prefix is served from the provider's prompt cache.
	if e.trunk != nil {
		res, cmeta := e.reviewCached(cctx, req, cm)
		e.fillTokenMeta(&cmeta, tracker)
		cmeta.model = meta.model
		return res, cmeta
	}

	res, cmeta := e.reviewSingleShot(cctx, req, cm)
	e.fillTokenMeta(&cmeta, tracker)
	cmeta.model = meta.model
	return res, cmeta
}

// reviewSingleShot is the V1 path: build the prompt, call Generate up to
// parseAttempts times to obtain strict JSON, and map it to a verdict. Any error,
// timeout, or unparseable output escalates to the user (fail-open to a human).
func (e *Engine) reviewSingleShot(ctx context.Context, req Request, cm einomodel.ToolCallingChatModel) (Result, reviewMeta) {
	meta := reviewMeta{}
	userPrompt := renderUserPrompt(req)
	var lastErr error
	for attempt := 0; attempt < parseAttempts; attempt++ {
		user := userPrompt
		if attempt > 0 {
			user += "\n\n(Your previous reply was not valid JSON. Reply with ONLY the JSON value.)"
		}
		meta.calls++
		out, err := cm.Generate(ctx, []*schema.Message{
			schema.SystemMessage(e.system),
			schema.UserMessage(user),
		})
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break // timeout / cancel: don't burn the remaining attempt
			}
			continue
		}
		if out == nil {
			lastErr = fmt.Errorf("nil model output")
			continue
		}
		a, ok := parseAssessment(out.Content)
		if !ok {
			lastErr = fmt.Errorf("unparseable output")
			continue
		}
		meta.userAuth = a.UserAuthorization
		if res, ok := mapOutcome(a); ok {
			return res, meta
		}
		lastErr = fmt.Errorf("missing/invalid outcome")
	}
	if lastErr != nil {
		config.Logger().Printf("[review] escalating to user: %v", lastErr)
		meta.failReason = lastErr.Error()
	}
	return Result{Outcome: Escalate, Failed: true}, meta
}

// mapOutcome converts a parsed assessment into a Result.
func mapOutcome(a assessment) (Result, bool) {
	switch strings.ToLower(strings.TrimSpace(a.Outcome)) {
	case "allow":
		return Result{Outcome: Allow, RiskLevel: a.RiskLevel, Rationale: a.Rationale}, true
	case "deny":
		return Result{Outcome: Deny, RiskLevel: a.RiskLevel, Rationale: a.Rationale}, true
	default:
		return Result{}, false
	}
}

func (e *Engine) fillTokenMeta(m *reviewMeta, tracker *internalmodel.TokenUsage) {
	d := tracker.GetFull()
	m.promptTokens = int64(d.PromptTokens)
	m.cachedTokens = int64(d.CachedTokens)
	m.cacheSeen = tracker.CacheObserved()
	if m.calls == 0 {
		m.calls = int64(d.CallCount)
	}
}

// String renders an Outcome for the audit log / logs.
func (o Outcome) String() string {
	switch o {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	default:
		return "escalate"
	}
}

// renderUserPrompt builds the evidence message: recent transcript, then the
// exact planned action.
func renderUserPrompt(req Request) string {
	var b strings.Builder
	b.WriteString("# Recent conversation (untrusted evidence)\n")
	if len(req.Transcript) == 0 {
		b.WriteString("(no transcript available)\n")
	} else {
		b.WriteString(renderTranscript(req.Transcript))
	}
	b.WriteString("\n# Planned action to judge\n")
	fmt.Fprintf(&b, "tool: %s\n", req.ToolName)
	if req.Cwd != "" {
		fmt.Fprintf(&b, "cwd: %s\n", req.Cwd)
	}
	fmt.Fprintf(&b, "touches_path_outside_workspace: %t\n", req.IsExternal)
	args := req.ToolArgs
	if len(args) > maxArgsChars {
		args = args[:maxArgsChars] + "\n…(truncated)"
	}
	b.WriteString("arguments:\n")
	b.WriteString(args)
	b.WriteString("\n")
	return b.String()
}

func renderTranscript(msgs []Msg) string {
	if len(msgs) > maxTranscriptMsgs {
		msgs = msgs[len(msgs)-maxTranscriptMsgs:]
	}
	var b strings.Builder
	for _, m := range msgs {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		if len(content) > maxMsgChars {
			content = content[:maxMsgChars] + "…(truncated)"
		}
		role := m.Role
		if role == "" {
			role = "unknown"
		}
		fmt.Fprintf(&b, "[%s] %s\n", role, content)
	}
	return b.String()
}
