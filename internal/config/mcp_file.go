package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// mcpFileNames are the standalone MCP config filenames searched in each
// .jcode/ directory and the project root. The format is compatible with
// Claude Desktop / Cursor mcp.json: { "mcpServers": { "name": { ... } } }.
var mcpFileNames = []string{"mcp.json", ".mcp.json"}

// mcpFileSchema is the on-disk format of a standalone mcp.json file.
type mcpFileSchema struct {
	MCPServers map[string]*MCPServer `json:"mcpServers"`
}

// LoadMCPFile reads a single standalone mcp.json file and returns its server
// map. Returns nil (without error) when the file does not exist.
func LoadMCPFile(path string) (map[string]*MCPServer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("mcp file read %s: %w", path, err)
	}
	var schema mcpFileSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("mcp file parse %s: %w", path, err)
	}
	return schema.MCPServers, nil
}

// LoadMCPFiles discovers and merges standalone mcp.json files from multiple
// locations (lowest → highest precedence):
//
//  1. ~/.jcode/mcp.json (global)
//  2. Walk-up: <gitRoot>/.jcode/mcp.json → ... → <pwd>/.jcode/mcp.json
//  3. <pwd>/mcp.json and <pwd>/.mcp.json (project root convenience)
//
// Servers are merged by name using the same rules as config.json MCP merge:
// later files can add new servers or override tuning fields of existing ones,
// but cannot change command/url of a server defined in an earlier layer.
//
// Returns nil when no mcp.json files exist anywhere in the chain.
func LoadMCPFiles(pwd string) (map[string]*MCPServer, error) {
	var gitRoot string
	if pwd != "" {
		gitRoot = GitRoot(pwd)
	}
	return loadMCPFilesWithRoot(pwd, gitRoot)
}

// loadMCPFilesWithRoot is the internal implementation that accepts a
// pre-resolved gitRoot to avoid redundant git subprocess calls.
func loadMCPFilesWithRoot(pwd, gitRoot string) (map[string]*MCPServer, error) {
	var merged map[string]*MCPServer

	merge := func(servers map[string]*MCPServer, source string) {
		if len(servers) == 0 {
			return
		}
		if merged == nil {
			merged = make(map[string]*MCPServer, len(servers))
		}
		for name, srv := range servers {
			if srv != nil && srv.Source == "" {
				srv.Source = source
			}
			if existing := merged[name]; existing != nil {
				mergeMCPServer(existing, srv)
			} else {
				merged[name] = srv
			}
		}
	}

	// 1. Global ~/.jcode/mcp.json
	for _, fname := range mcpFileNames {
		servers, err := LoadMCPFile(filepath.Join(ConfigDir(), fname))
		if err != nil {
			return nil, err
		}
		merge(servers, "global")
	}

	// 2. Walk-up .jcode/mcp.json from git root to pwd
	if pwd != "" {
		dirs := ConfigWalkDirs(gitRoot, pwd)
		for _, dir := range dirs {
			for _, fname := range mcpFileNames {
				servers, err := LoadMCPFile(filepath.Join(dir, configDir, fname))
				if err != nil {
					return nil, err
				}
				merge(servers, "project")
			}
		}

		// 3. Project root convenience: <pwd>/mcp.json, <pwd>/.mcp.json
		for _, fname := range mcpFileNames {
			servers, err := LoadMCPFile(filepath.Join(pwd, fname))
			if err != nil {
				return nil, err
			}
			merge(servers, "project")
		}
	}

	return merged, nil
}

// MergeMCPServers merges standalone mcp.json servers into the config's
// MCPServers map. The same security rules apply: new servers are added freely,
// existing servers only get tuning-field overrides (not command/url).
func MergeMCPServers(cfg *Config, servers map[string]*MCPServer) {
	if cfg == nil || len(servers) == 0 {
		return
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]*MCPServer, len(servers))
	}
	for name, srv := range servers {
		if existing := cfg.MCPServers[name]; existing != nil {
			mergeMCPServer(existing, srv)
		} else {
			cfg.MCPServers[name] = srv
		}
	}
}
