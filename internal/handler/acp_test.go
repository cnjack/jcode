package handler

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/cnjack/jcode/internal/mode"
)

func TestACPAllowAllPromotionFailureDoesNotPublishMode(t *testing.T) {
	h := NewACPHandler(nil, "sess", t.TempDir())
	called := false
	h.SetModeChangeCallback(func(got mode.SessionMode) error {
		called = true
		if got != mode.FullAccess {
			t.Fatalf("promotion mode=%v", got)
		}
		return errors.New("journal unavailable")
	})
	if err := h.notifyModeChanged(mode.FullAccess); !errors.Is(err, ErrApprovalModePromotion) {
		t.Fatalf("promotion error=%v", err)
	}
	if !called {
		t.Fatal("durable promotion callback was not invoked")
	}
}

func TestACPToolResultArtifactMetadataIsBoundedAndResolvesEnginePath(t *testing.T) {
	long := strings.Repeat("x", maxACPMetadataString+100)
	refs := make([]ArtifactRef, maxACPResultArtifacts+2)
	for i := range refs {
		refs[i] = ArtifactRef{
			ID: long, Storage: "managed", Key: "images/generated.png",
			Title: long, Kind: "image", MediaType: "image/png", Size: 123,
			Width: 32, Height: 16, Provider: "bigmodel", Model: "cogview",
		}
	}
	resolvedPath := filepath.Join(string(filepath.Separator), "tmp", "jcode", "generated.png")
	raw := acpToolResultRawOutput(
		context.Background(), "saved", refs,
		func(_ context.Context, ref ArtifactRef) (string, error) {
			if ref.Storage != "managed" {
				t.Fatalf("resolver got storage %q", ref.Storage)
			}
			return resolvedPath, nil
		},
	)
	obj, ok := raw.(map[string]any)
	if !ok || obj["text"] != "saved" || obj["artifacts_truncated"] != true {
		t.Fatalf("raw output = %#v", raw)
	}
	artifacts, ok := obj["artifacts"].([]map[string]any)
	if !ok || len(artifacts) != maxACPResultArtifacts {
		t.Fatalf("artifacts = %#v", obj["artifacts"])
	}
	if artifacts[0]["engine_path"] != resolvedPath {
		t.Fatalf("engine path = %#v", artifacts[0]["engine_path"])
	}
	if got := artifacts[0]["id"].(string); len(got) != maxACPMetadataString {
		t.Fatalf("bounded id len = %d", len(got))
	}
	if got := artifacts[0]["title"].(string); len(got) != maxACPMetadataString {
		t.Fatalf("bounded title len = %d", len(got))
	}
	receipt := acpArtifactReceipt(artifacts[:1], false)
	for _, want := range []string{
		"bigmodel / cogview", "32x16", "123 bytes", "JCode engine path: " + resolvedPath,
	} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("receipt %q missing %q", receipt, want)
		}
	}
	if strings.Contains(receipt, strings.Repeat("x", maxACPMetadataString+1)) {
		t.Fatal("receipt contained unbounded metadata")
	}
	visible := acpArtifactReceiptContent(artifacts[:1], false)
	if len(visible) != 1 || visible[0].Content == nil ||
		visible[0].Content.Content.Text == nil ||
		visible[0].Content.Content.Text.Text != receipt {
		t.Fatalf("visible ACP content = %#v", visible)
	}
}

func TestACPToolResultArtifactMetadataOmitsUnverifiedPath(t *testing.T) {
	ref := ArtifactRef{ID: "artifact", Storage: "managed", Kind: "image"}
	for _, resolver := range []func(context.Context, ArtifactRef) (string, error){
		func(context.Context, ArtifactRef) (string, error) { return "relative/path", nil },
		func(context.Context, ArtifactRef) (string, error) { return "", context.Canceled },
	} {
		raw := acpToolResultRawOutput(context.Background(), "", []ArtifactRef{ref}, resolver)
		artifacts := raw.(map[string]any)["artifacts"].([]map[string]any)
		if _, exists := artifacts[0]["engine_path"]; exists {
			t.Fatalf("unverified engine path leaked: %#v", artifacts[0])
		}
	}
	if raw := acpToolResultRawOutput(context.Background(), "legacy", nil, nil); raw != "legacy" {
		t.Fatalf("legacy raw output changed: %#v", raw)
	}
}

func TestACPToolPresentationReadUsesFriendlyTitleAndAbsoluteLocation(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator), "tmp", "jcode-work")
	h := NewACPHandler(nil, "sess", workDir)

	p := h.presentationForTool("read", `{"file_path":"internal/handler/acp.go","offset":10,"limit":5}`)

	if p.Title != "Read internal/handler/acp.go (10-14)" {
		t.Fatalf("title = %q", p.Title)
	}
	if p.Kind != acp.ToolKindRead {
		t.Fatalf("kind = %q", p.Kind)
	}
	if len(p.Locations) != 1 {
		t.Fatalf("locations len = %d", len(p.Locations))
	}
	wantPath := filepath.Join(workDir, "internal", "handler", "acp.go")
	if p.Locations[0].Path != wantPath {
		t.Fatalf("location path = %q, want %q", p.Locations[0].Path, wantPath)
	}
	if p.Locations[0].Line == nil || *p.Locations[0].Line != 10 {
		t.Fatalf("location line = %v, want 10", p.Locations[0].Line)
	}
}

func TestACPToolPresentationSearchAndExecute(t *testing.T) {
	h := NewACPHandler(nil, "sess", "/repo")

	grep := h.presentationForTool("grep", `{"pattern":"ToolCall","path":"internal"}`)
	if grep.Title != `Search "ToolCall" in internal` {
		t.Fatalf("grep title = %q", grep.Title)
	}
	if grep.Kind != acp.ToolKindSearch {
		t.Fatalf("grep kind = %q", grep.Kind)
	}

	exec := h.presentationForTool("execute", `{"command":"go test ./...","description":"Run all tests"}`)
	if exec.Title != "Run all tests" {
		t.Fatalf("execute title = %q", exec.Title)
	}
	if exec.Kind != acp.ToolKindExecute {
		t.Fatalf("execute kind = %q", exec.Kind)
	}
}

func TestBillableACPPresentationContainsOnlyBoundedSummary(t *testing.T) {
	summary := &BillableApprovalSummary{
		Capability: "image.generate", Provider: "xai", Model: "grok-imagine-image",
		AspectRatio: "9:16", Resolution: "2k", Count: 1, Billable: true,
	}
	presentation := billableACPPresentation(summary)
	if presentation.Title != "Generate image with xai / grok-imagine-image" {
		t.Fatalf("title = %q", presentation.Title)
	}
	input, ok := presentation.RawInput.(map[string]any)
	if !ok || input["capability"] != "image.generate" || input["provider"] != "xai" ||
		input["aspect_ratio"] != "9:16" || input["resolution"] != "2k" {
		t.Fatalf("raw input = %#v", presentation.RawInput)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "prompt") || strings.Contains(string(encoded), "credential") {
		t.Fatalf("billable presentation exposed request data: %s", encoded)
	}
}

func TestACPBillablePermissionEchoesOnlyExactRunnerOption(t *testing.T) {
	allowID := "runner-allow-once"
	denyID := "runner-deny"
	optionSet, err := buildACPPermissionOptions(ApprovalRequest{
		ApprovalClass:   "billable_external",
		AllowApproveAll: true,
		Options: []ApprovalOption{
			{ID: allowID, Label: "Allow once", Kind: "allow_once"},
			{ID: denyID, Label: "Deny", Kind: "deny"},
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if optionSet.allowOnceID != allowID || optionSet.rejectOnceID != denyID ||
		optionSet.allowAlwaysID != "" || len(optionSet.options) != 2 ||
		string(optionSet.options[0].OptionId) != allowID || string(optionSet.options[1].OptionId) != denyID {
		t.Fatalf("ACP replaced or expanded runner options: %#v", optionSet)
	}
	allow, err := resolveACPPermissionOption(allowID, allowID, denyID, "", false)
	if err != nil || !allow.Approved || allow.Mode != ModeManual || allow.ResolvedOptionID != allowID {
		t.Fatalf("allow response=%#v err=%v", allow, err)
	}
	deny, err := resolveACPPermissionOption(denyID, allowID, denyID, "", false)
	if err != nil || deny.Approved || deny.Mode != ModeManual || deny.ResolvedOptionID != denyID {
		t.Fatalf("deny response=%#v err=%v", deny, err)
	}
	for _, selected := range []string{"", "forged", "allow-always"} {
		if response, resolveErr := resolveACPPermissionOption(
			selected, allowID, denyID, "allow-always", false,
		); resolveErr == nil || response.Approved {
			t.Fatalf("selected %q response=%#v err=%v", selected, response, resolveErr)
		}
	}
}

func TestACPToolPresentationWriteIncludesDiffContent(t *testing.T) {
	h := NewACPHandler(nil, "sess", "/repo")

	p := h.presentationForTool("write", `{"file_path":"README.md","content":"hello"}`)

	if p.Title != "Write README.md" {
		t.Fatalf("title = %q", p.Title)
	}
	if len(p.Content) != 1 || p.Content[0].Diff == nil {
		t.Fatalf("expected one diff content item, got %#v", p.Content)
	}
	if p.Content[0].Diff.Path != "README.md" {
		t.Fatalf("diff path = %q", p.Content[0].Diff.Path)
	}
}

func TestACPSubagentNameFromArgs(t *testing.T) {
	if got := subagentNameFromArgs(`{"name":"scan-repo","prompt":"..."}`); got != "scan-repo" {
		t.Fatalf("name = %q, want scan-repo", got)
	}
	if got := subagentNameFromArgs(`not json`); got != "" {
		t.Fatalf("name = %q, want empty for invalid JSON", got)
	}
	if got := subagentNameFromArgs(`{"prompt":"..."}`); got != "" {
		t.Fatalf("name = %q, want empty when absent", got)
	}
}

func TestACPSubagentProgressLine(t *testing.T) {
	if got := subagentProgressLine("tool_call", "grep", `{"pattern":"foo"}`); got != `→ grep {"pattern":"foo"}` {
		t.Fatalf("tool_call line = %q", got)
	}
	if got := subagentProgressLine("tool_result", "read", "line one\nline two"); got != "← read line one line two" {
		t.Fatalf("tool_result line = %q", got)
	}
	long := strings.Repeat("x", 500)
	if got := subagentProgressLine("tool_result", "read", long); len(got) > 200 {
		t.Fatalf("long detail not truncated: len=%d", len(got))
	}
}

func TestACPSubagentDoneClearsMappingWithoutUpdate(t *testing.T) {
	// nil conn: the test passes only if the done path never touches the
	// connection (it must only clear the progress mapping).
	h := NewACPHandler(nil, "sess", "/repo")
	h.subagentCalls["scan-repo"] = "tc_1"

	h.OnSubagentEvent("scan-repo", "explore", true, "result", nil)

	if _, ok := h.subagentCalls["scan-repo"]; ok {
		t.Fatal("done event did not clear subagent mapping")
	}
	// Unknown subagent progress must be a silent no-op (no conn access).
	h.OnSubagentProgress("scan-repo", "tool_call", "grep", "{}")
}

func TestACPToolResultClearsStaleSubagentMapping(t *testing.T) {
	h := NewACPHandler(nil, "sess", "/repo")
	h.einoToACP["eino_1"] = "tc_1"
	h.subagentCalls["scan-repo"] = "tc_1"
	// Terminal status already sent (e.g. permission rejection): OnToolResult
	// returns before sending, but must still drop the stale mapping.
	h.toolTerminated["tc_1"] = true

	h.OnToolResult(ToolResultEvent{Name: "subagent", ToolCallID: "eino_1"})

	if _, ok := h.subagentCalls["scan-repo"]; ok {
		t.Fatal("tool result did not clear stale subagent mapping")
	}
}

func TestACPToolFailureOutputDetection(t *testing.T) {
	cases := []string{
		"Tool execution failed: exit status 1",
		"partial output\n\nTool execution failed: exit status 1",
		"Tool execution panicked: boom",
	}
	for _, tc := range cases {
		if !isToolFailureOutput(tc) {
			t.Fatalf("expected failure output for %q", tc)
		}
	}
	if isToolFailureOutput(strings.TrimSpace("command completed")) {
		t.Fatal("did not expect normal output to be treated as failure")
	}
}
