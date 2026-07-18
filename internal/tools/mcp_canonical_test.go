package tools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
)

type mcpTestCallOptions struct {
	marker string
}

type recordingMCPInvokableTool struct {
	info         *schema.ToolInfo
	result       string
	err          error
	arguments    string
	optionMarker string
}

func newRecordingMCPTool(name string) *recordingMCPInvokableTool {
	return &recordingMCPInvokableTool{
		info: &schema.ToolInfo{
			Name: name,
			Desc: "description for " + name,
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"value": {Type: schema.String, Required: true},
			}),
		},
		result: "result for " + name,
	}
}

func (t *recordingMCPInvokableTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *recordingMCPInvokableTool) InvokableRun(
	_ context.Context,
	argumentsInJSON string,
	opts ...tool.Option,
) (string, error) {
	t.arguments = argumentsInJSON
	callOptions := tool.GetImplSpecificOptions(&mcpTestCallOptions{}, opts...)
	t.optionMarker = callOptions.marker
	return t.result, t.err
}

type recordingMCPEnhancedTool struct {
	info     *schema.ToolInfo
	argument *schema.ToolArgument
	result   *schema.ToolResult
}

func (t *recordingMCPEnhancedTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *recordingMCPEnhancedTool) InvokableRun(
	_ context.Context,
	argument *schema.ToolArgument,
	_ ...tool.Option,
) (*schema.ToolResult, error) {
	t.argument = argument
	return t.result, nil
}

func TestCanonicalizeMCPToolsPreservesRawEndpointAndOwnership(t *testing.T) {
	ctx := context.Background()
	endpoint := newRecordingMCPTool("get.item-name")
	tools, counts, errs := canonicalizeMCPTools(ctx, []mcpServerToolSet{
		{serverName: "Some-Server", tools: []tool.BaseTool{endpoint}},
	})

	if len(errs) != 0 {
		t.Fatalf("canonicalizeMCPTools() errors = %v", errs)
	}
	if counts["Some-Server"] != 1 || len(tools) != 1 {
		t.Fatalf("counts = %v, tools = %d", counts, len(tools))
	}
	info, err := tools[0].Info(ctx)
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	const canonicalName = "mcp__Some_Server__get_item_name"
	if info.Name != canonicalName {
		t.Fatalf("canonical name = %q, want %q", info.Name, canonicalName)
	}
	if info.Desc != endpoint.info.Desc || info.ParamsOneOf != endpoint.info.ParamsOneOf {
		t.Fatal("canonical wrapper did not preserve tool description/schema")
	}
	if endpoint.info.Name != "get.item-name" {
		t.Fatalf("raw endpoint name was mutated to %q", endpoint.info.Name)
	}

	identity, ok := MCPToolIdentityForTool(canonicalName)
	if !ok {
		t.Fatalf("MCPToolIdentityForTool(%q) not found", canonicalName)
	}
	wantIdentity := MCPToolIdentity{
		CanonicalName: canonicalName,
		ServerName:    "Some-Server",
		OriginalName:  "get.item-name",
		DisplayName:   "Some-Server.get.item-name",
	}
	if identity != wantIdentity {
		t.Fatalf("identity = %#v, want %#v", identity, wantIdentity)
	}
	if server, ok := MCPServerForTool(canonicalName); !ok || server != "Some-Server" {
		t.Fatalf("MCPServerForTool() = %q, %v", server, ok)
	}
	if rawName, ok := MCPOriginalToolName(canonicalName); !ok || rawName != "get.item-name" {
		t.Fatalf("MCPOriginalToolName() = %q, %v", rawName, ok)
	}
	if display, ok := MCPDisplayNameForTool(canonicalName); !ok || display != "Some-Server.get.item-name" {
		t.Fatalf("MCPDisplayNameForTool() = %q, %v", display, ok)
	}

	invokable, ok := tools[0].(tool.InvokableTool)
	if !ok {
		t.Fatalf("canonical wrapper type %T is not tool.InvokableTool", tools[0])
	}
	option := tool.WrapImplSpecificOptFn(func(options *mcpTestCallOptions) {
		options.marker = "kept"
	})
	result, err := invokable.InvokableRun(ctx, `{"value":"raw"}`, option)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	if result != endpoint.result || endpoint.arguments != `{"value":"raw"}` || endpoint.optionMarker != "kept" {
		t.Fatalf("delegation result=%q arguments=%q option=%q", result, endpoint.arguments, endpoint.optionMarker)
	}
}

func TestCanonicalizeMCPToolsSupportsEnhancedEndpoint(t *testing.T) {
	ctx := context.Background()
	wantResult := &schema.ToolResult{}
	endpoint := &recordingMCPEnhancedTool{
		info:   &schema.ToolInfo{Name: "enhanced-tool"},
		result: wantResult,
	}
	tools, _, errs := canonicalizeMCPTools(ctx, []mcpServerToolSet{
		{serverName: "enhanced-server", tools: []tool.BaseTool{endpoint}},
	})
	if len(errs) != 0 || len(tools) != 1 {
		t.Fatalf("canonicalizeMCPTools() tools=%d errors=%v", len(tools), errs)
	}
	enhanced, ok := tools[0].(tool.EnhancedInvokableTool)
	if !ok {
		t.Fatalf("canonical wrapper type %T is not tool.EnhancedInvokableTool", tools[0])
	}
	argument := &schema.ToolArgument{Text: `{"value":1}`}
	result, err := enhanced.InvokableRun(ctx, argument)
	if err != nil || result != wantResult || endpoint.argument != argument {
		t.Fatalf("enhanced delegation result=%p error=%v argument=%p", result, err, endpoint.argument)
	}
}

func TestCanonicalMCPToolProjectsStructuredResultForModel(t *testing.T) {
	tests := []struct {
		name   string
		result string
	}{
		{
			name: "compatibility fallback is ignored",
			result: ` {
				"content": [{"type":"text","text":"opaque fallback"}],
				"structuredContent": { "ok": true, "value": 42 }
			} `,
		},
		{
			name:   "empty content is valid",
			result: `{"content":[],"structuredContent":{"ok":true,"value":42}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			endpoint := newRecordingMCPTool("lookup")
			endpoint.result = tt.result
			wrapped := canonicalInvokableForTest(ctx, t, endpoint)

			got, err := wrapped.InvokableRun(ctx, `{"value":"raw"}`)
			if err != nil {
				t.Fatalf("InvokableRun() error = %v", err)
			}
			if want := `{"ok":true,"value":42}`; got != want {
				t.Fatalf("InvokableRun() = %q, want structured projection %q", got, want)
			}
		})
	}
}

func TestCanonicalMCPToolProjectsContentWithoutStructuredResult(t *testing.T) {
	tests := []struct {
		name   string
		result string
	}{
		{
			name:   "missing structured content",
			result: `{"content": [ {"type":"text", "text":"done"} ]}`,
		},
		{
			name:   "null structured content",
			result: `{"content": [ {"type":"text", "text":"done"} ], "structuredContent": null}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := newRecordingMCPTool("lookup")
			endpoint.result = tt.result
			wrapped := canonicalInvokableForTest(context.Background(), t, endpoint)

			got, err := wrapped.InvokableRun(context.Background(), `{}`)
			if err != nil {
				t.Fatalf("InvokableRun() error = %v", err)
			}
			if want := `[{"type":"text","text":"done"}]`; got != want {
				t.Fatalf("InvokableRun() = %q, want content projection %q", got, want)
			}
		})
	}
}

func TestCanonicalMCPToolLeavesUnrecognizedResultsUntouched(t *testing.T) {
	tests := []struct {
		name   string
		result string
	}{
		{name: "plain text", result: "plain result"},
		{name: "malformed JSON", result: `{"content":[`},
		{name: "ordinary JSON", result: `{"ok":true}`},
		{name: "non-array content", result: `{"content":"done","structuredContent":{"ok":true}}`},
		{name: "unfolded MCP error", result: `{"content":[],"structuredContent":{"ok":false},"isError":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := newRecordingMCPTool("lookup")
			endpoint.result = tt.result
			wrapped := canonicalInvokableForTest(context.Background(), t, endpoint)

			got, err := wrapped.InvokableRun(context.Background(), `{}`)
			if err != nil {
				t.Fatalf("InvokableRun() error = %v", err)
			}
			if got != tt.result {
				t.Fatalf("InvokableRun() = %q, want unchanged %q", got, tt.result)
			}
		})
	}
}

func TestCanonicalMCPToolPreservesEndpointError(t *testing.T) {
	endpoint := newRecordingMCPTool("lookup")
	endpoint.result = `{"content":[],"structuredContent":{"ok":false}}`
	endpoint.err = errors.New("endpoint failed")
	wrapped := canonicalInvokableForTest(context.Background(), t, endpoint)

	got, err := wrapped.InvokableRun(context.Background(), `{}`)
	if !errors.Is(err, endpoint.err) || got != endpoint.result {
		t.Fatalf("InvokableRun() result=%q error=%v, want original result/error", got, err)
	}
}

func TestCanonicalizeMCPToolsSameRawNameAcrossServers(t *testing.T) {
	tools, _, errs := canonicalizeMCPTools(context.Background(), []mcpServerToolSet{
		{serverName: "beta", tools: []tool.BaseTool{newRecordingMCPTool("lookup")}},
		{serverName: "alpha", tools: []tool.BaseTool{newRecordingMCPTool("lookup")}},
	})
	if len(errs) != 0 {
		t.Fatalf("canonicalizeMCPTools() errors = %v", errs)
	}
	want := []string{"mcp__alpha__lookup", "mcp__beta__lookup"}
	if got := modelVisibleMCPToolNames(t, tools); !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical names = %v, want %v", got, want)
	}
}

func TestCanonicalizeMCPToolsDisambiguatesServerSanitizeCollisionStably(t *testing.T) {
	first := canonicalNamesByRawIdentity(t, []mcpServerToolSet{
		{serverName: "basic-server", tools: []tool.BaseTool{newRecordingMCPTool("lookup")}},
		{serverName: "basic_server", tools: []tool.BaseTool{newRecordingMCPTool("query")}},
	})
	second := canonicalNamesByRawIdentity(t, []mcpServerToolSet{
		{serverName: "basic_server", tools: []tool.BaseTool{newRecordingMCPTool("query")}},
		{serverName: "basic-server", tools: []tool.BaseTool{newRecordingMCPTool("lookup")}},
	})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("canonical names changed with input order: first=%v second=%v", first, second)
	}
	wantFirst := "mcp__basic_server" + mcpHashSuffix("basic-server") + "__lookup"
	wantSecond := "mcp__basic_server" + mcpHashSuffix("basic_server") + "__query"
	if first["basic-server\x00lookup"] != wantFirst || first["basic_server\x00query"] != wantSecond {
		t.Fatalf("server collision names = %v, want %q and %q", first, wantFirst, wantSecond)
	}
}

func TestCanonicalizeMCPToolsDisambiguatesToolSanitizeCollisionStably(t *testing.T) {
	first := canonicalNamesByRawIdentity(t, []mcpServerToolSet{
		{serverName: "server", tools: []tool.BaseTool{
			newRecordingMCPTool("tool-name"),
			newRecordingMCPTool("tool_name"),
		}},
	})
	second := canonicalNamesByRawIdentity(t, []mcpServerToolSet{
		{serverName: "server", tools: []tool.BaseTool{
			newRecordingMCPTool("tool_name"),
			newRecordingMCPTool("tool-name"),
		}},
	})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("canonical names changed with input order: first=%v second=%v", first, second)
	}
	for _, rawName := range []string{"tool-name", "tool_name"} {
		rawIdentity := "server\x00" + rawName
		want := "mcp__server__tool_name" + mcpHashSuffix(rawIdentity)
		if first[rawIdentity] != want {
			t.Fatalf("canonical name for %q = %q, want %q", rawName, first[rawIdentity], want)
		}
	}
}

func TestCanonicalizeMCPToolsLongNamesAreValidAndStable(t *testing.T) {
	serverName := strings.Repeat("server-", 15)
	rawNames := []string{
		strings.Repeat("first-tool-", 12),
		strings.Repeat("second-tool-", 12),
	}
	sets := []mcpServerToolSet{{
		serverName: serverName,
		tools: []tool.BaseTool{
			newRecordingMCPTool(rawNames[0]),
			newRecordingMCPTool(rawNames[1]),
		},
	}}
	first := canonicalNamesByRawIdentity(t, sets)
	second := canonicalNamesByRawIdentity(t, []mcpServerToolSet{{
		serverName: serverName,
		tools: []tool.BaseTool{
			newRecordingMCPTool(rawNames[1]),
			newRecordingMCPTool(rawNames[0]),
		},
	}})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("long canonical names are unstable: first=%v second=%v", first, second)
	}
	seen := make(map[string]struct{})
	for identity, name := range first {
		assertValidMCPModelName(t, name)
		if len(name) != maxMCPToolNameLength {
			t.Fatalf("long canonical name %q has %d bytes, want %d", name, len(name), maxMCPToolNameLength)
		}
		if _, duplicate := seen[name]; duplicate {
			t.Fatalf("long names collided: %q (%s)", name, identity)
		}
		seen[name] = struct{}{}
	}
}

func TestCanonicalizeMCPToolsSkipsExactDuplicateAndKeepsToolSearchNamespace(t *testing.T) {
	tools, counts, errs := canonicalizeMCPTools(context.Background(), []mcpServerToolSet{
		{serverName: "server", tools: []tool.BaseTool{
			newRecordingMCPTool("tool_search"),
			newRecordingMCPTool("tool_search"),
		}},
	})
	if len(errs) != 0 {
		t.Fatalf("canonicalizeMCPTools() errors = %v", errs)
	}
	if len(tools) != 1 || counts["server"] != 1 {
		t.Fatalf("tools=%d counts=%v, want one exact identity", len(tools), counts)
	}
	if got := modelVisibleMCPToolNames(t, tools); !reflect.DeepEqual(got, []string{"mcp__server__tool_search"}) {
		t.Fatalf("canonical names = %v", got)
	}
}

func TestSortedEnabledMCPServerNames(t *testing.T) {
	got := sortedEnabledMCPServerNames(map[string]*config.MCPServer{
		"zeta":     {},
		"alpha":    {},
		"disabled": {Disabled: true},
		"nil":      nil,
	})
	want := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedEnabledMCPServerNames() = %v, want %v", got, want)
	}
}

func canonicalNamesByRawIdentity(t *testing.T, sets []mcpServerToolSet) map[string]string {
	t.Helper()
	tools, _, errs := canonicalizeMCPTools(context.Background(), sets)
	if len(errs) != 0 {
		t.Fatalf("canonicalizeMCPTools() errors = %v", errs)
	}
	result := make(map[string]string, len(tools))
	for _, endpoint := range tools {
		info, err := endpoint.Info(context.Background())
		if err != nil {
			t.Fatalf("Info() error = %v", err)
		}
		identity, ok := MCPToolIdentityForTool(info.Name)
		if !ok {
			t.Fatalf("identity for %q not found", info.Name)
		}
		result[identity.ServerName+"\x00"+identity.OriginalName] = info.Name
	}
	return result
}

func modelVisibleMCPToolNames(t *testing.T, tools []tool.BaseTool) []string {
	t.Helper()
	names := make([]string, 0, len(tools))
	for _, endpoint := range tools {
		info, err := endpoint.Info(context.Background())
		if err != nil {
			t.Fatalf("Info() error = %v", err)
		}
		names = append(names, info.Name)
		assertValidMCPModelName(t, info.Name)
	}
	return names
}

func canonicalInvokableForTest(
	ctx context.Context,
	t *testing.T,
	endpoint *recordingMCPInvokableTool,
) tool.InvokableTool {
	t.Helper()
	tools, _, errs := canonicalizeMCPTools(ctx, []mcpServerToolSet{
		{serverName: "server", tools: []tool.BaseTool{endpoint}},
	})
	if len(errs) != 0 || len(tools) != 1 {
		t.Fatalf("canonicalizeMCPTools() tools=%d errors=%v", len(tools), errs)
	}
	wrapped, ok := tools[0].(tool.InvokableTool)
	if !ok {
		t.Fatalf("canonical wrapper type %T is not tool.InvokableTool", tools[0])
	}
	return wrapped
}

func assertValidMCPModelName(t *testing.T, name string) {
	t.Helper()
	if len(name) == 0 || len(name) > maxMCPToolNameLength {
		t.Fatalf("model-visible MCP name %q has invalid byte length %d", name, len(name))
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		t.Fatalf("model-visible MCP name %q has invalid byte %q", name, ch)
	}
}
