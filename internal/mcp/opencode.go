package mcp

import (
	"encoding/json"
	"fmt"
	"os"

	"syncai/internal/util"
)

// openCodeAdapter handles OpenCode's opencode.json. The MCP servers
// live under the top-level "mcp" key alongside other OpenCode settings
// (model, agents, etc.). We preserve unrelated keys verbatim by reading
// the file as a map of json.RawMessage and only swapping out "mcp".
//
// Each entry shape:
//
//	"name": {
//	  "type": "local",                 // or "remote"
//	  "command": ["npx", "-y", "..."], // local only — array, not string
//	  "environment": {"K": "V"},       // local only — note the spelling
//	  "url": "https://...",            // remote only
//	  "headers": {...},                // remote only
//	  "enabled": true                  // optional
//	}
type openCodeAdapter struct{}

type openCodeServer struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
}

func (a *openCodeAdapter) Parse(path string) (map[string]Server, error) {
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
	raw, ok := doc["mcp"]
	if !ok || len(raw) == 0 {
		return map[string]Server{}, nil
	}
	var entries map[string]openCodeServer
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse %s.mcp: %w", path, err)
	}

	servers := make(map[string]Server, len(entries))
	for name, e := range entries {
		// Skip explicitly disabled servers — don't propagate them.
		if e.Enabled != nil && !*e.Enabled {
			continue
		}
		s := Server{
			Type:    e.Type,
			Env:     e.Environment,
			URL:     e.URL,
			Headers: e.Headers,
		}
		if len(e.Command) > 0 {
			s.Command = e.Command[0]
			if len(e.Command) > 1 {
				s.Args = e.Command[1:]
			}
		}
		servers[name] = s
	}
	return servers, nil
}

func (a *openCodeAdapter) Write(path string, servers map[string]Server) error {
	entries := make(map[string]openCodeServer, len(servers))
	for _, name := range sortedKeys(servers) {
		s := servers[name]
		entry := openCodeServer{}
		if s.IsRemote() {
			entry.Type = "remote"
			entry.URL = s.URL
			entry.Headers = s.Headers
		} else {
			entry.Type = "local"
			cmd := make([]string, 0, 1+len(s.Args))
			if s.Command != "" {
				cmd = append(cmd, s.Command)
			}
			cmd = append(cmd, s.Args...)
			entry.Command = cmd
			entry.Environment = s.Env
		}
		entries[name] = entry
	}

	// Preserve unrelated top-level keys.
	doc := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	mcpRaw, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal mcp: %w", err)
	}
	doc["mcp"] = mcpRaw

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	out = append(out, '\n')
	return util.WriteFile(path, out)
}
