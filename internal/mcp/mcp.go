// Package mcp adapts MCP server configuration between the on-disk shapes
// each AI agent expects.
//
// Every supported agent stores its MCP servers in a different format —
// dedicated files (Cursor, Copilot, Claude) vs shared config files
// (Codex, OpenCode), JSON vs TOML, distinct top-level keys, distinct
// field names. This package normalizes those differences so the syncai
// engine can treat MCP config as just another sync kind.
package mcp

import (
	"sort"
	"strings"

	"syncai/internal/model"
)

// Server is the shape-agnostic representation of an MCP server entry.
// Adapters parse each agent's on-disk schema into this shape and emit
// each agent's schema from it.
type Server struct {
	// Type is "stdio", "http", or "sse". Empty means "infer": stdio if
	// Command is set, http if URL is set.
	Type    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
}

// EffectiveType returns Type, falling back to a sensible default
// inferred from the populated fields.
func (s Server) EffectiveType() string {
	t := strings.ToLower(strings.TrimSpace(s.Type))
	switch t {
	case "stdio", "http", "sse":
		return t
	case "local":
		return "stdio"
	case "remote":
		return "http"
	}
	if s.URL != "" {
		return "http"
	}
	return "stdio"
}

// IsRemote is true when the server speaks over a network transport.
func (s Server) IsRemote() bool {
	t := s.EffectiveType()
	return t == "http" || t == "sse"
}

// Adapter reads and writes the MCP servers section of an agent's
// on-disk config file.
type Adapter interface {
	// Parse loads MCP servers from path. A missing file is treated as
	// "no servers" and returns an empty map without error.
	Parse(path string) (map[string]Server, error)
	// Write replaces the MCP servers section of path with servers,
	// preserving any non-MCP content in shared config files.
	Write(path string, servers map[string]Server) error
}

// GetAdapter returns the adapter for the given agent name, or nil
// if the agent does not have a known MCP schema.
func GetAdapter(agentName string) Adapter {
	switch strings.ToLower(strings.TrimSpace(agentName)) {
	case model.AgentCursor:
		// Cursor's traditional schema omits "type" for stdio entries
		// and uses {url} for remote — the same JSON shape we emit by
		// default works fine.
		return &dedicatedJSON{topLevelKey: "mcpServers"}
	case model.AgentClaude:
		return &dedicatedJSON{topLevelKey: "mcpServers", emitType: true}
	case model.AgentCopilot:
		// GitHub Copilot CLI: ~/.copilot/mcp-config.json (or any path
		// passed via --mcp-config). Top-level key is "mcpServers" —
		// distinct from VS Code Copilot's `.vscode/mcp.json` which
		// uses "servers".
		return &dedicatedJSON{topLevelKey: "mcpServers", emitType: true}
	case model.AgentCodex:
		return &codexAdapter{}
	case model.AgentOpenCode:
		return &openCodeAdapter{}
	}
	return nil
}

func sortedKeys(m map[string]Server) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
