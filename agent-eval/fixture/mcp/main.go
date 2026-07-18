// Command jcode-mcp-fixture is a deterministic stdio MCP server used by the
// ToolSearch evaluation suite. It deliberately exposes a large, similar-looking
// catalog while keeping one unambiguous target tool for routing assertions.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const targetToolName = "catalog_lookup_precise"

var supportedToolCounts = map[int]struct{}{
	10:  {},
	30:  {},
	50:  {},
	100: {},
}

var similarToolNames = []string{
	targetToolName,
	"catalog_lookup_inventory",
	"catalog_lookup_metadata",
	"catalog_lookup_policy",
	"catalog_lookup_preview",
	"catalog_lookup_price",
	"catalog_lookup_recent",
	"catalog_lookup_supplier",
	"catalog_search_precise",
	"customer_catalog_lookup",
}

type fixtureCall struct {
	Sequence  int            `json:"sequence"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	Marker    string         `json:"marker"`
}

// fixtureResponse makes successful completion explicit to the model. The
// campaign evaluates routing, not whether a model can infer completion from an
// opaque sentinel, so the sentinel remains as evidence while the response also
// resembles a real structured catalog result.
type fixtureResponse struct {
	Status         string        `json:"status"`
	Complete       bool          `json:"complete"`
	Authoritative  bool          `json:"authoritative"`
	RequestID      string        `json:"request_id"`
	Query          string        `json:"query"`
	RequestedLimit any           `json:"requested_limit"`
	Record         fixtureRecord `json:"record"`
	Marker         string        `json:"marker"`
}

type fixtureRecord struct {
	ExternalSKU string `json:"external_sku"`
	Source      string `json:"source"`
}

type callLogger struct {
	mu       sync.Mutex
	path     string
	sequence int
}

func (l *callLogger) append(toolName string, arguments map[string]any, marker string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sequence++
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open fixture call log: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("secure fixture call log: %w", err)
	}

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(fixtureCall{
		Sequence: l.sequence, Tool: toolName, Arguments: arguments, Marker: marker,
	}); err != nil {
		return fmt.Errorf("append fixture call log: %w", err)
	}
	return nil
}

func fixtureToolNames(count int) ([]string, error) {
	if _, ok := supportedToolCounts[count]; !ok {
		return nil, fmt.Errorf("unsupported tool count %d (want one of 10, 30, 50, 100)", count)
	}

	names := append([]string(nil), similarToolNames...)
	for i := 1; len(names) < count; i++ {
		names = append(names, fmt.Sprintf("fixture_utility_%03d", i))
	}
	sort.Strings(names)
	return names, nil
}

func toolDescription(name string) string {
	switch name {
	case targetToolName:
		return "Fetch the authoritative catalog record for an exact external SKU. " +
			"This is the only fixture tool that returns the required precise lookup result."
	case "catalog_lookup_preview":
		return "Preview a draft catalog record; not authoritative and not suitable for exact SKU lookup."
	case "catalog_lookup_recent":
		return "List recently viewed catalog records; it does not perform an exact SKU lookup."
	case "catalog_search_precise":
		return "Run full-text catalog search with precise ranking; it does not fetch by exact SKU."
	default:
		return "Deterministic catalog-adjacent fixture operation used as a routing distractor."
	}
}

func newFixtureTool(name string) mcp.Tool {
	return mcp.NewTool(
		name,
		mcp.WithDescription(toolDescription(name)),
		mcp.WithString("request_id", mcp.Required(), mcp.Description("Unique fixture request identifier")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Exact fixture lookup query")),
		mcp.WithInteger("limit", mcp.Min(1), mcp.Max(100), mcp.Description("Maximum records to return")),
	)
}

func markerFor(toolName string, arguments map[string]any) string {
	raw, _ := json.Marshal(arguments)
	sum := sha256.Sum256(raw)
	requestID, _ := arguments["request_id"].(string)
	return fmt.Sprintf("JCODE_MCP_FIXTURE_OK:%s:%s:%x", toolName, requestID, sum[:8])
}

func responseFor(arguments map[string]any, marker string) fixtureResponse {
	requestID, _ := arguments["request_id"].(string)
	query, _ := arguments["query"].(string)
	return fixtureResponse{
		Status:         "found",
		Complete:       true,
		Authoritative:  true,
		RequestID:      requestID,
		Query:          query,
		RequestedLimit: arguments["limit"],
		Record: fixtureRecord{
			ExternalSKU: query,
			Source:      "jcode-toolsearch-fixture",
		},
		Marker: marker,
	}
}

func resultFor(toolName string, arguments map[string]any, marker string) *mcp.CallToolResult {
	if toolName != targetToolName {
		return mcp.NewToolResultText(marker)
	}
	return mcp.NewToolResultStructured(responseFor(arguments, marker), marker)
}

func newFixtureServer(toolCount int, logger *callLogger) (*server.MCPServer, error) {
	names, err := fixtureToolNames(toolCount)
	if err != nil {
		return nil, err
	}

	srv := server.NewMCPServer(
		"jcode-toolsearch-fixture",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithInputSchemaValidation(),
	)
	for _, name := range names {
		toolName := name
		srv.AddTool(newFixtureTool(toolName), func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			arguments := request.GetArguments()
			marker := markerFor(toolName, arguments)
			if err := logger.append(toolName, arguments, marker); err != nil {
				return nil, err
			}
			return resultFor(toolName, arguments, marker), nil
		})
	}
	return srv, nil
}

func run(toolCount int, logPath string) error {
	if logPath == "" {
		return errors.New("--log is required so calls never contaminate MCP stdout")
	}
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("create fixture log directory: %w", err)
	}
	if err := os.Chmod(logDir, 0o700); err != nil {
		return fmt.Errorf("secure fixture log directory: %w", err)
	}
	srv, err := newFixtureServer(toolCount, &callLogger{path: logPath})
	if err != nil {
		return err
	}
	return server.ServeStdio(srv)
}

func main() {
	toolCount := flag.Int("count", 10, "number of tools to expose: 10, 30, 50, or 100")
	logPath := flag.String("log", "", "JSONL call log path (required)")
	flag.Parse()

	if err := run(*toolCount, *logPath); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "jcode-mcp-fixture:", err)
		os.Exit(1)
	}
}
