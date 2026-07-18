package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	mcpp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	mcpToolNamePrefix    = "mcp__"
	mcpToolNameDelimiter = "__"
	maxMCPToolNameLength = 64
	mcpToolHashLength    = 12
)

// MCPToolIdentity keeps the model-visible MCP name reversible. ServerName and
// OriginalName are the unmodified values used for the actual MCP protocol call.
type MCPToolIdentity struct {
	CanonicalName string
	ServerName    string
	OriginalName  string
	DisplayName   string
}

// mcpToolIdentities maps a model-visible name to its raw MCP endpoint.
// Hot reloads overwrite current identities with the same canonical name.
var mcpToolIdentities sync.Map

// RegisterMCPToolIdentity records canonical model name, server and raw endpoint
// name together. Keeping this API explicit prevents a raw MCP name such as
// "grep" from being mistaken for model-visible provenance and changing the
// approval policy of an unrelated built-in tool.
func RegisterMCPToolIdentity(canonicalName, serverName, originalName string) {
	if canonicalName == "" || serverName == "" || originalName == "" {
		return
	}
	registerMCPToolIdentity(MCPToolIdentity{
		CanonicalName: canonicalName,
		ServerName:    serverName,
		OriginalName:  originalName,
		DisplayName:   serverName + "." + originalName,
	})
}

func registerMCPToolIdentity(identity MCPToolIdentity) {
	if identity.CanonicalName == "" || identity.ServerName == "" || identity.OriginalName == "" {
		return
	}
	if identity.DisplayName == "" {
		identity.DisplayName = identity.ServerName + "." + identity.OriginalName
	}
	mcpToolIdentities.Store(identity.CanonicalName, identity)
}

// MCPToolIdentityForTool returns the raw endpoint and display metadata for a
// canonical (or legacy registered) MCP tool name.
func MCPToolIdentityForTool(toolName string) (MCPToolIdentity, bool) {
	v, ok := mcpToolIdentities.Load(toolName)
	if !ok {
		return MCPToolIdentity{}, false
	}
	identity, ok := v.(MCPToolIdentity)
	return identity, ok
}

// MCPServerForTool returns the server providing toolName, if it is a known MCP
// tool.
func MCPServerForTool(toolName string) (string, bool) {
	identity, ok := MCPToolIdentityForTool(toolName)
	if !ok || identity.ServerName == "" {
		return "", false
	}
	return identity.ServerName, true
}

// MCPOriginalToolName returns the unmodified tool name sent to the MCP server.
func MCPOriginalToolName(toolName string) (string, bool) {
	identity, ok := MCPToolIdentityForTool(toolName)
	if !ok || identity.OriginalName == "" {
		return "", false
	}
	return identity.OriginalName, true
}

// MCPDisplayNameForTool returns the human-readable server.tool label.
func MCPDisplayNameForTool(toolName string) (string, bool) {
	identity, ok := MCPToolIdentityForTool(toolName)
	if !ok || identity.DisplayName == "" {
		return "", false
	}
	return identity.DisplayName, true
}

type MCPStatus struct {
	Name      string
	ToolCount int
	Error     error
	Running   bool
	// NeedsAuth is true when the server returned an OAuth authorization
	// challenge — the user must log in before tools become available.
	NeedsAuth bool
}

type mcpServerToolSet struct {
	serverName string
	tools      []tool.BaseTool
}

// LoadMCPTools establishes connections to configured MCP servers and fetches
// their tools. Servers are connected in name order and their model-visible tool
// names are normalized as one catalog so sanitization collisions are stable.
func LoadMCPTools(ctx context.Context, mcpConfig map[string]*config.MCPServer) ([]tool.BaseTool, []MCPStatus) {
	serverNames := sortedEnabledMCPServerNames(mcpConfig)
	statuses := make([]MCPStatus, 0, len(serverNames))
	loaded := make([]mcpServerToolSet, 0, len(serverNames))
	statusIndexes := make(map[string]int, len(serverNames))

	for _, name := range serverNames {
		srv := mcpConfig[name]
		status := MCPStatus{Name: name}

		cli, err := buildMCPClient(name, srv)
		if err != nil {
			status.Error = fmt.Errorf("client create failed: %w", err)
			statuses = append(statuses, status)
			continue
		}

		if err := cli.Start(ctx); err != nil {
			if isMCPAuthError(err) {
				status.NeedsAuth = true
				status.Error = fmt.Errorf("authorization required — run login")
			} else {
				status.Error = fmt.Errorf("start failed: %w", err)
			}
			statuses = append(statuses, status)
			continue
		}

		initReq := mcp.InitializeRequest{}
		initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initReq.Params.ClientInfo = mcp.Implementation{
			Name:    "little-jack",
			Version: "1.0.0",
		}

		if _, err := cli.Initialize(ctx, initReq); err != nil {
			if isMCPAuthError(err) {
				status.NeedsAuth = true
				status.Error = fmt.Errorf("authorization required — run login")
			} else {
				status.Error = fmt.Errorf("init failed: %w", err)
			}
			statuses = append(statuses, status)
			continue
		}

		ts, err := mcpp.GetTools(ctx, &mcpp.Config{Cli: cli})
		if err != nil {
			status.Error = fmt.Errorf("fetch tools failed: %w", err)
			statuses = append(statuses, status)
			continue
		}

		statusIndexes[name] = len(statuses)
		statuses = append(statuses, status)
		loaded = append(loaded, mcpServerToolSet{serverName: name, tools: ts})
	}

	canonicalTools, toolCounts, canonicalErrors := canonicalizeMCPTools(ctx, loaded)
	for _, server := range loaded {
		status := &statuses[statusIndexes[server.serverName]]
		if err := canonicalErrors[server.serverName]; err != nil {
			status.Error = fmt.Errorf("prepare tools failed: %w", err)
			continue
		}
		status.Running = true
		status.ToolCount = toolCounts[server.serverName]
	}

	return canonicalTools, statuses
}

func sortedEnabledMCPServerNames(mcpConfig map[string]*config.MCPServer) []string {
	serverNames := make([]string, 0, len(mcpConfig))
	for name, server := range mcpConfig {
		if server != nil && !server.Disabled {
			serverNames = append(serverNames, name)
		}
	}
	sort.Strings(serverNames)
	return serverNames
}

type mcpToolCandidate struct {
	serverName  string
	rawName     string
	rawIdentity string
	serverPart  string
	toolPart    string
	info        *schema.ToolInfo
	endpoint    tool.BaseTool
}

func canonicalizeMCPTools(
	ctx context.Context,
	sets []mcpServerToolSet,
) ([]tool.BaseTool, map[string]int, map[string]error) {
	candidates, serverErrors := collectMCPToolCandidates(ctx, sets)
	disambiguateMCPServerParts(candidates)
	disambiguateMCPToolParts(candidates)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].rawIdentity < candidates[j].rawIdentity
	})

	result := make([]tool.BaseTool, 0, len(candidates))
	toolCounts := make(map[string]int, len(sets))
	usedNames := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		canonicalName := uniqueCanonicalMCPToolName(candidate, usedNames)
		wrapped, err := wrapCanonicalMCPTool(candidate.endpoint, candidate.info, canonicalName)
		if err != nil {
			serverErrors[candidate.serverName] = err
			continue
		}
		identity := MCPToolIdentity{
			CanonicalName: canonicalName,
			ServerName:    candidate.serverName,
			OriginalName:  candidate.rawName,
			DisplayName:   candidate.serverName + "." + candidate.rawName,
		}
		registerMCPToolIdentity(identity)
		result = append(result, wrapped)
		toolCounts[candidate.serverName]++
	}

	if len(serverErrors) == 0 {
		return result, toolCounts, serverErrors
	}
	filtered := result[:0]
	for _, wrapped := range result {
		info, err := wrapped.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		serverName, ok := MCPServerForTool(info.Name)
		if ok && serverErrors[serverName] == nil {
			filtered = append(filtered, wrapped)
		}
	}
	for serverName := range serverErrors {
		toolCounts[serverName] = 0
	}
	return filtered, toolCounts, serverErrors
}

func collectMCPToolCandidates(
	ctx context.Context,
	sets []mcpServerToolSet,
) ([]*mcpToolCandidate, map[string]error) {
	candidates := make([]*mcpToolCandidate, 0)
	serverErrors := make(map[string]error)
	seenRawIdentities := make(map[string]struct{})
	for _, set := range sets {
		serverCandidates := make([]*mcpToolCandidate, 0, len(set.tools))
		for _, endpoint := range set.tools {
			info, err := endpoint.Info(ctx)
			if err != nil {
				serverErrors[set.serverName] = fmt.Errorf("read tool info: %w", err)
				break
			}
			if info == nil || info.Name == "" {
				serverErrors[set.serverName] = fmt.Errorf("tool returned empty info or name")
				break
			}
			if !isExecutableMCPTool(endpoint) {
				serverErrors[set.serverName] = fmt.Errorf("tool %q is not invokable", info.Name)
				break
			}
			rawIdentity := set.serverName + "\x00" + info.Name
			if _, duplicate := seenRawIdentities[rawIdentity]; duplicate {
				continue
			}
			seenRawIdentities[rawIdentity] = struct{}{}
			serverCandidates = append(serverCandidates, &mcpToolCandidate{
				serverName:  set.serverName,
				rawName:     info.Name,
				rawIdentity: rawIdentity,
				serverPart:  sanitizeMCPToolNamePart(set.serverName),
				toolPart:    sanitizeMCPToolNamePart(info.Name),
				info:        info,
				endpoint:    endpoint,
			})
		}
		if serverErrors[set.serverName] == nil {
			candidates = append(candidates, serverCandidates...)
		}
	}
	return candidates, serverErrors
}

func isExecutableMCPTool(endpoint tool.BaseTool) bool {
	if _, ok := endpoint.(tool.InvokableTool); ok {
		return true
	}
	_, ok := endpoint.(tool.EnhancedInvokableTool)
	return ok
}

func sanitizeMCPToolNamePart(name string) string {
	var sanitized strings.Builder
	sanitized.Grow(len(name))
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_' {
			sanitized.WriteRune(ch)
		} else {
			sanitized.WriteByte('_')
		}
	}
	if sanitized.Len() == 0 {
		return "_"
	}
	return sanitized.String()
}

func disambiguateMCPServerParts(candidates []*mcpToolCandidate) {
	identitiesByPart := make(map[string]map[string]struct{})
	for _, candidate := range candidates {
		if identitiesByPart[candidate.serverPart] == nil {
			identitiesByPart[candidate.serverPart] = make(map[string]struct{})
		}
		identitiesByPart[candidate.serverPart][candidate.serverName] = struct{}{}
	}
	for _, candidate := range candidates {
		if len(identitiesByPart[candidate.serverPart]) > 1 {
			candidate.serverPart = appendMCPHashSuffix(candidate.serverPart, candidate.serverName)
		}
	}
}

func disambiguateMCPToolParts(candidates []*mcpToolCandidate) {
	identitiesByPart := make(map[string]map[string]struct{})
	for _, candidate := range candidates {
		key := candidate.serverPart + "\x00" + candidate.toolPart
		if identitiesByPart[key] == nil {
			identitiesByPart[key] = make(map[string]struct{})
		}
		identitiesByPart[key][candidate.rawIdentity] = struct{}{}
	}
	for _, candidate := range candidates {
		key := candidate.serverPart + "\x00" + candidate.toolPart
		if len(identitiesByPart[key]) > 1 {
			candidate.toolPart = appendMCPHashSuffix(candidate.toolPart, candidate.rawIdentity)
		}
	}
}

func uniqueCanonicalMCPToolName(candidate *mcpToolCandidate, used map[string]struct{}) string {
	baseName := mcpToolNamePrefix + candidate.serverPart + mcpToolNameDelimiter + candidate.toolPart
	if len(baseName) <= maxMCPToolNameLength {
		if _, exists := used[baseName]; !exists {
			used[baseName] = struct{}{}
			return baseName
		}
	}

	for attempt := 0; ; attempt++ {
		hashInput := candidate.rawIdentity
		if attempt > 0 {
			hashInput += "\x00" + strconv.Itoa(attempt)
		}
		name := fitCanonicalMCPToolName(candidate.serverPart, candidate.toolPart, hashInput)
		if _, exists := used[name]; exists {
			continue
		}
		used[name] = struct{}{}
		return name
	}
}

func fitCanonicalMCPToolName(serverPart, toolPart, rawIdentity string) string {
	namespace := mcpToolNamePrefix + serverPart
	suffix := mcpHashSuffix(rawIdentity)
	maxToolLength := maxMCPToolNameLength - len(namespace) - len(mcpToolNameDelimiter)
	if maxToolLength >= len(suffix) {
		toolPrefixLength := maxToolLength - len(suffix)
		return namespace + mcpToolNameDelimiter + truncateMCPToolNamePart(toolPart, toolPrefixLength) + suffix
	}

	maxNamespaceLength := maxMCPToolNameLength - len(mcpToolNameDelimiter) - len(suffix)
	return truncateMCPToolNamePart(namespace, maxNamespaceLength) + mcpToolNameDelimiter + suffix
}

func appendMCPHashSuffix(value, rawIdentity string) string {
	return value + mcpHashSuffix(rawIdentity)
}

func mcpHashSuffix(rawIdentity string) string {
	digest := sha256.Sum256([]byte(rawIdentity))
	hexDigest := hex.EncodeToString(digest[:])
	return "_" + hexDigest[:mcpToolHashLength]
}

func truncateMCPToolNamePart(value string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength]
}

type canonicalMCPInvokableTool struct {
	endpoint tool.InvokableTool
	info     *schema.ToolInfo
}

func (t *canonicalMCPInvokableTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *canonicalMCPInvokableTool) InvokableRun(
	ctx context.Context,
	argumentsInJSON string,
	opts ...tool.Option,
) (string, error) {
	return t.endpoint.InvokableRun(ctx, argumentsInJSON, opts...)
}

type canonicalMCPEnhancedInvokableTool struct {
	endpoint tool.EnhancedInvokableTool
	info     *schema.ToolInfo
}

func (t *canonicalMCPEnhancedInvokableTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *canonicalMCPEnhancedInvokableTool) InvokableRun(
	ctx context.Context,
	argument *schema.ToolArgument,
	opts ...tool.Option,
) (*schema.ToolResult, error) {
	return t.endpoint.InvokableRun(ctx, argument, opts...)
}

func wrapCanonicalMCPTool(
	endpoint tool.BaseTool,
	info *schema.ToolInfo,
	canonicalName string,
) (tool.BaseTool, error) {
	canonicalInfo := *info
	canonicalInfo.Name = canonicalName
	if invokable, ok := endpoint.(tool.InvokableTool); ok {
		return &canonicalMCPInvokableTool{endpoint: invokable, info: &canonicalInfo}, nil
	}
	if enhanced, ok := endpoint.(tool.EnhancedInvokableTool); ok {
		return &canonicalMCPEnhancedInvokableTool{endpoint: enhanced, info: &canonicalInfo}, nil
	}
	return nil, fmt.Errorf("tool %q is not invokable", info.Name)
}
