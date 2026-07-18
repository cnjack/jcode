package main

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestFixtureToolNamesSupportedSizesAreStable(t *testing.T) {
	for _, count := range []int{10, 30, 50, 100} {
		first, err := fixtureToolNames(count)
		if err != nil {
			t.Fatalf("fixtureToolNames(%d): %v", count, err)
		}
		second, err := fixtureToolNames(count)
		if err != nil {
			t.Fatalf("fixtureToolNames(%d) second call: %v", count, err)
		}
		if len(first) != count {
			t.Fatalf("fixtureToolNames(%d) returned %d tools", count, len(first))
		}
		if !sort.StringsAreSorted(first) {
			t.Fatalf("fixtureToolNames(%d) is not sorted: %v", count, first)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("fixtureToolNames(%d) is unstable", count)
		}
		seenTarget := false
		for i, name := range first {
			if i > 0 && name == first[i-1] {
				t.Fatalf("fixtureToolNames(%d) contains duplicate %q", count, name)
			}
			seenTarget = seenTarget || name == targetToolName
		}
		if !seenTarget {
			t.Fatalf("fixtureToolNames(%d) omitted target %q", count, targetToolName)
		}
	}
}

func TestFixtureToolNamesRejectsUnsupportedSize(t *testing.T) {
	if _, err := fixtureToolNames(11); err == nil {
		t.Fatal("fixtureToolNames(11) unexpectedly succeeded")
	}
}

func TestMarkerForIsStableAndArgumentSensitive(t *testing.T) {
	args := map[string]any{"request_id": "req-1", "query": "sku-42", "limit": float64(3)}
	first := markerFor(targetToolName, args)
	second := markerFor(targetToolName, map[string]any{
		"limit": float64(3), "query": "sku-42", "request_id": "req-1",
	})
	if first != second {
		t.Fatalf("marker changed with map order: %q != %q", first, second)
	}
	changed := markerFor(targetToolName, map[string]any{
		"request_id": "req-1", "query": "sku-43", "limit": float64(3),
	})
	if first == changed {
		t.Fatalf("marker did not change with arguments: %q", first)
	}
}

func TestFixtureServerListsSortedToolsAndLogsTargetCall(t *testing.T) {
	logPath := t.TempDir() + "/calls.jsonl"
	srv, err := newFixtureServer(30, &callLogger{path: logPath})
	if err != nil {
		t.Fatalf("newFixtureServer: %v", err)
	}
	cli, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	defer func() { _ = cli.Close() }()
	if err := cli.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "fixture-test", Version: "1"}
	if _, err := cli.Initialize(t.Context(), initRequest); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	listed, err := cli.ListTools(t.Context(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) != 30 {
		t.Fatalf("ListTools returned %d tools, want 30", len(listed.Tools))
	}
	listedNames := make([]string, len(listed.Tools))
	for i, tool := range listed.Tools {
		listedNames[i] = tool.Name
	}
	if !sort.StringsAreSorted(listedNames) {
		t.Fatalf("ListTools is not sorted: %v", listedNames)
	}

	arguments := map[string]any{"request_id": "req-live", "query": "sku-42", "limit": 3}
	result, err := cli.CallTool(t.Context(), mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params: mcp.CallToolParams{
			Name: targetToolName, Arguments: arguments,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	marker := markerFor(targetToolName, arguments)
	rendered, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(rendered), marker) {
		t.Fatalf("CallTool result=%s error=%v, want marker %q", rendered, err, marker)
	}
	secondArguments := map[string]any{"request_id": "req-live-2", "query": "sku-43", "limit": 1}
	if _, err := cli.CallTool(t.Context(), mcp.CallToolRequest{
		Request: mcp.Request{Method: "tools/call"},
		Params: mcp.CallToolParams{
			Name: targetToolName, Arguments: secondArguments,
		},
	}); err != nil {
		t.Fatalf("second CallTool: %v", err)
	}

	rawLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(call log): %v", err)
	}
	logInfo, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Stat(call log): %v", err)
	}
	if got := logInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("call log permission = %#o, want 0600", got)
	}
	lines := strings.Split(strings.TrimSpace(string(rawLog)), "\n")
	if len(lines) != 2 {
		t.Fatalf("call log has %d lines, want 2: %q", len(lines), rawLog)
	}
	var logged, loggedSecond fixtureCall
	if err := json.Unmarshal([]byte(lines[0]), &logged); err != nil {
		t.Fatalf("Unmarshal(first call log line): %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &loggedSecond); err != nil {
		t.Fatalf("Unmarshal(second call log line): %v", err)
	}
	loggedArgs, _ := json.Marshal(logged.Arguments)
	wantArgs, _ := json.Marshal(arguments)
	if logged.Tool != targetToolName || logged.Marker != marker || string(loggedArgs) != string(wantArgs) {
		t.Fatalf("logged call=%#v, want tool=%q args=%v marker=%q", logged, targetToolName, arguments, marker)
	}
	if logged.Sequence != 1 || loggedSecond.Sequence != 2 || loggedSecond.Marker != markerFor(targetToolName, secondArguments) {
		t.Fatalf("call log did not append stable sequence: first=%#v second=%#v", logged, loggedSecond)
	}
}
