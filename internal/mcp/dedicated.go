package mcp

import (
	"encoding/json"
	"fmt"
	"os"

	"syncai/internal/util"
)

// dedicatedJSON handles agents whose MCP config lives in a dedicated
// JSON file: Cursor (.cursor/mcp.json), Claude (.mcp.json), and Copilot
// (.vscode/mcp.json). They differ only in the top-level key name and
// whether the "type" field should be emitted explicitly for stdio
// entries.
type dedicatedJSON struct {
	topLevelKey string // "mcpServers" or "servers"
	emitType    bool   // emit "type": "stdio" for stdio entries (Claude/Copilot)
}

// jsonServer mirrors the on-disk shape used by Cursor, Claude, and
// Copilot. Field declaration order determines the order on disk.
type jsonServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (a *dedicatedJSON) Parse(path string) (map[string]Server, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Server{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return map[string]Server{}, nil
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	raw, ok := doc[a.topLevelKey]
	if !ok || len(raw) == 0 {
		return map[string]Server{}, nil
	}
	var entries map[string]jsonServer
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse %s.%s: %w", path, a.topLevelKey, err)
	}

	servers := make(map[string]Server, len(entries))
	for name, e := range entries {
		servers[name] = Server{
			Type:    e.Type,
			Command: e.Command,
			Args:    e.Args,
			Env:     e.Env,
			URL:     e.URL,
			Headers: e.Headers,
		}
	}
	return servers, nil
}

func (a *dedicatedJSON) Write(path string, servers map[string]Server) error {
	entries := make(map[string]jsonServer, len(servers))
	for _, name := range sortedKeys(servers) {
		s := servers[name]
		entry := jsonServer{
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
			URL:     s.URL,
			Headers: s.Headers,
		}
		if a.emitType {
			entry.Type = s.EffectiveType()
		} else if t := s.EffectiveType(); t != "stdio" {
			// For Cursor: omit type for stdio (legacy default), emit
			// for remote so newer Cursor versions parse correctly.
			entry.Type = t
		}
		entries[name] = entry
	}

	doc := map[string]any{a.topLevelKey: entries}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	return util.WriteFile(path, data)
}
