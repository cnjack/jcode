package hooks

import (
	"context"
	"encoding/json"
	"os"
)

// Dispatcher fires the hooks configured for an event and folds their outcomes
// into a single Decision. Implementations must be safe for concurrent use.
type Dispatcher interface {
	// Fire runs every matching hook for event with the given payload and returns
	// the folded decision. The zero Decision means "no opinion — proceed".
	Fire(ctx context.Context, event Event, p Payload) Decision
	// Configured reports whether any hook is registered for event, letting hot
	// paths skip payload construction when hooks are unused.
	Configured(event Event) bool
}

// Options parameterize a Dispatcher with per-session context and hooks.
type Options struct {
	CWD            string
	SessionID      string
	TranscriptPath string
	Env            []string // base env; nil → os.Environ()
	Logf           func(string, ...any)
}

// NewDispatcher builds a Dispatcher from a merged Config. When the config defines
// no hooks it returns a no-op dispatcher whose Fire is free, so callers can wire
// it unconditionally.
func NewDispatcher(cfg Config, opts Options) Dispatcher {
	if cfg.Empty() {
		return nopDispatcher{}
	}
	env := opts.Env
	if env == nil {
		env = os.Environ()
	}
	return &dispatcher{
		cfg:            cfg,
		cwd:            opts.CWD,
		sessionID:      opts.SessionID,
		transcriptPath: opts.TranscriptPath,
		baseEnv:        env,
		logf:           opts.Logf,
	}
}

type dispatcher struct {
	cfg            Config
	cwd            string
	sessionID      string
	transcriptPath string
	baseEnv        []string
	logf           func(string, ...any)
}

func (d *dispatcher) Configured(event Event) bool {
	return len(d.cfg.Hooks[string(event)]) > 0
}

func (d *dispatcher) Fire(ctx context.Context, event Event, p Payload) Decision {
	specs := d.selectSpecs(event, p.ToolName)
	if len(specs) == 0 {
		return Decision{}
	}

	// Fill session-scoped payload defaults.
	p.HookEventName = string(event)
	if p.CWD == "" {
		p.CWD = d.cwd
	}
	if p.SessionID == "" {
		p.SessionID = d.sessionID
	}
	if p.TranscriptPath == "" {
		p.TranscriptPath = d.transcriptPath
	}

	var dec Decision
	for _, s := range specs {
		if s.Async && !event.Blockable() {
			// Fire-and-forget: cannot influence the decision. Detach from ctx so a
			// turn ending mid-notification does not truncate it, but bound it with a
			// hard cap so a hung async hook cannot leak a goroutine indefinitely.
			payload := p
			spec := s
			env := hookEnv(d.baseEnv, payload)
			input := mustJSON(payload)
			go func() {
				actx, acancel := context.WithTimeout(context.WithoutCancel(ctx), asyncHardCap)
				defer acancel()
				runHook(actx, spec, input, payload.CWD, env, event, d.logf)
			}()
			continue
		}

		out := runHook(ctx, s, mustJSON(p), p.CWD, hookEnv(d.baseEnv, p), event, d.logf)
		fold(&dec, out, event, &p)

		// Once denied/blocked, later hooks cannot un-deny and any input rewrite is
		// moot — stop early.
		if dec.Permission == PermDeny || dec.Block {
			break
		}
	}
	return dec
}

// selectSpecs returns the flattened hook specs whose matcher applies.
func (d *dispatcher) selectSpecs(event Event, toolName string) []HookSpec {
	groups := d.cfg.Hooks[string(event)]
	if len(groups) == 0 {
		return nil
	}
	var specs []HookSpec
	for _, g := range groups {
		if !matchesTool(g.Matcher, toolName) {
			continue
		}
		for _, h := range g.Hooks {
			// v1 only supports command hooks; empty type defaults to command.
			if h.Type != "" && h.Type != "command" {
				if d.logf != nil {
					d.logf("hooks: unsupported hook type %q ignored", h.Type)
				}
				continue
			}
			specs = append(specs, h)
		}
	}
	return specs
}

// fold merges one hook's outcome into the running Decision and chains any input /
// result rewrite into the payload so the next hook sees the updated value.
func fold(dec *Decision, out runOutcome, event Event, p *Payload) {
	dec.Permission = upgradePerm(dec.Permission, out.permission)
	if out.block {
		dec.Block = true
	}
	if event == PreToolUse && dec.Permission == PermDeny {
		dec.Block = true
	}
	if (out.block || out.permission == PermDeny) && out.reason != "" && dec.Reason == "" {
		dec.Reason = out.reason
	}

	if out.updatedInput != nil {
		dec.UpdatedInput = out.updatedInput
		p.ToolInput = out.updatedInput // chain into the next hook
	}
	if out.modifiedResult != nil {
		dec.ModifiedResult = out.modifiedResult
		p.ToolResponse = *out.modifiedResult // chain into the next hook
	}
	if out.additionalContext != "" {
		dec.AdditionalContext = joinLines(dec.AdditionalContext, out.additionalContext)
	}
	if out.systemMessage != "" {
		dec.SystemMessage = joinLines(dec.SystemMessage, out.systemMessage)
	}
}

// upgradePerm folds two permission verdicts with precedence deny > ask > allow.
func upgradePerm(cur, next Permission) Permission {
	if rank(next) > rank(cur) {
		return next
	}
	return cur
}

func rank(p Permission) int {
	switch p {
	case PermAllow:
		return 1
	case PermAsk:
		return 2
	case PermDeny:
		return 3
	default:
		return 0
	}
}

func joinLines(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n" + b
}

func mustJSON(p Payload) []byte {
	data, err := json.Marshal(p)
	if err != nil {
		// The only field that can make marshalling fail is ToolInput (a
		// json.RawMessage holding non-JSON tool args). Drop it and retry so the
		// rest of the payload — tool_name, event, cwd — still reaches the hook.
		p.ToolInput = nil
		if data, err = json.Marshal(p); err != nil {
			return []byte("{}")
		}
	}
	return data
}

// nopDispatcher is returned when no hooks are configured.
type nopDispatcher struct{}

func (nopDispatcher) Fire(context.Context, Event, Payload) Decision { return Decision{} }
func (nopDispatcher) Configured(Event) bool                         { return false }
