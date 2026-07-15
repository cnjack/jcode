package review

import (
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/config"
)

func TestParseAssessment(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantOK   bool
		wantOut  string
		wantRisk string
	}{
		{"clean", `{"risk_level":"high","user_authorization":"low","outcome":"deny","rationale":"x"}`, true, "deny", "high"},
		{"fast-path allow", `{"outcome":"allow"}`, true, "allow", ""},
		{"fenced", "```json\n{\"outcome\":\"deny\",\"risk_level\":\"critical\"}\n```", true, "deny", "critical"},
		{"prose-wrapped", "Sure, here is my verdict:\n{\"outcome\":\"allow\"}\nHope that helps.", true, "allow", ""},
		{"nested braces in rationale string", `{"outcome":"deny","rationale":"deletes {config} dir"}`, true, "deny", ""},
		{"missing outcome", `{"risk_level":"low"}`, false, "", ""},
		{"not json", `I think this is fine, allow it.`, false, "", ""},
		{"empty", ``, false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := parseAssessment(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v (in=%q)", ok, tc.wantOK, tc.in)
			}
			if !ok {
				return
			}
			if a.Outcome != tc.wantOut {
				t.Errorf("outcome=%q want %q", a.Outcome, tc.wantOut)
			}
			if tc.wantRisk != "" && a.RiskLevel != tc.wantRisk {
				t.Errorf("risk=%q want %q", a.RiskLevel, tc.wantRisk)
			}
		})
	}
}

func TestMapOutcome(t *testing.T) {
	cases := []struct {
		outcome string
		want    Outcome
		ok      bool
	}{
		{"allow", Allow, true},
		{"ALLOW", Allow, true},
		{" deny ", Deny, true},
		{"escalate", Escalate, true},
		{"ESCALATE", Escalate, true},
		{"maybe", Escalate, false},
		{"", Escalate, false},
	}
	for _, tc := range cases {
		res, ok := mapOutcome(assessment{Outcome: tc.outcome})
		if ok != tc.ok {
			t.Errorf("%q: ok=%v want %v", tc.outcome, ok, tc.ok)
		}
		if ok && res.Outcome != tc.want {
			t.Errorf("%q: outcome=%v want %v", tc.outcome, res.Outcome, tc.want)
		}
	}
}

func TestOutcomeString(t *testing.T) {
	if Allow.String() != "allow" || Deny.String() != "deny" || Escalate.String() != "escalate" {
		t.Fatalf("unexpected Outcome.String values")
	}
}

func TestResolveModelRef(t *testing.T) {
	cases := []struct {
		name     string
		override string
		small    string
		main     string
		want     string
	}{
		{"explicit override wins", "prov/reviewer", "prov/small", "prov/main", "prov/reviewer"},
		{"small alias resolves to small_model", "small", "prov/small", "prov/main", "prov/small"},
		{"empty falls back to small_model", "", "prov/small", "prov/main", "prov/small"},
		{"unset small falls back to main", "", "", "prov/main", "prov/main"},
		{"small alias with unset small falls to main", "small", "", "prov/main", "prov/main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &Engine{
				cfg:           &config.Config{SmallModel: tc.small, Model: tc.main},
				modelOverride: tc.override,
			}
			if got := e.resolveModelRef(); got != tc.want {
				t.Errorf("resolveModelRef()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestRenderUserPrompt_BoundsAndContent(t *testing.T) {
	big := strings.Repeat("A", maxArgsChars+500)
	req := Request{
		ToolName:   "execute",
		ToolArgs:   `{"command":"` + big + `"}`,
		Cwd:        "/work",
		IsExternal: true,
		Transcript: []Msg{{Role: "user", Content: "please delete the temp dir"}},
	}
	out := renderUserPrompt(req)
	if !strings.Contains(out, "tool: execute") {
		t.Errorf("missing tool name")
	}
	if !strings.Contains(out, "touches_path_outside_workspace: true") {
		t.Errorf("missing external flag")
	}
	if !strings.Contains(out, "please delete the temp dir") {
		t.Errorf("missing transcript content")
	}
	if !strings.Contains(out, "(truncated)") {
		t.Errorf("expected oversized args to be truncated")
	}
}

func TestRenderTranscript_CapsMessages(t *testing.T) {
	msgs := make([]Msg, maxTranscriptMsgs+10)
	for i := range msgs {
		msgs[i] = Msg{Role: "user", Content: "m"}
	}
	// The oldest should be dropped; count rendered lines.
	out := renderTranscript(msgs)
	lines := strings.Count(out, "\n")
	if lines > maxTranscriptMsgs {
		t.Errorf("rendered %d lines, expected <= %d", lines, maxTranscriptMsgs)
	}
}

func TestBuildSystemPrompt_IncludesExtraPolicy(t *testing.T) {
	sp := buildSystemPrompt("Never touch prod DB.")
	if !strings.Contains(sp, "Additional workspace policy") || !strings.Contains(sp, "Never touch prod DB.") {
		t.Errorf("extra policy not embedded")
	}
	if !strings.Contains(sp, "STRICT JSON") {
		t.Errorf("output contract missing")
	}
}

func TestNew_BuildFromConfigAlwaysReturnsReviewer(t *testing.T) {
	if r := BuildFromConfig(&config.Config{}, "darwin"); r == nil {
		t.Errorf("expected non-nil reviewer with empty config")
	}
	if r := BuildFromConfig(&config.Config{
		ApprovalReview: &config.ApprovalReviewConfig{Model: "small"},
		Model:          "prov/main",
	}, "darwin"); r == nil {
		t.Errorf("expected non-nil reviewer with approval_review settings")
	}
}
