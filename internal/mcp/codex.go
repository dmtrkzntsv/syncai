package mcp

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"syncai/internal/util"
)

// codexAdapter handles OpenAI Codex CLI's `.codex/config.toml`. That
// file contains many other Codex settings besides MCP, so we can't
// re-serialize the whole document — we'd lose comments and unknown
// keys. Instead we rewrite at the text level: identify and strip every
// `[mcp_servers...]` table, then append fresh ones.
//
// Codex's stable schema only supports stdio MCP servers
// (`command`, `args`, `env`). Servers that look remote are skipped on
// write with a warning.
type codexAdapter struct{}

func (a *codexAdapter) Parse(path string) (map[string]Server, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Server{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	servers := map[string]Server{}
	blocks := scanMCPBlocks(string(data))
	for name, body := range blocks {
		s := parseCodexBlock(body)
		if s.Command == "" && s.URL == "" {
			continue
		}
		servers[name] = s
	}
	return servers, nil
}

func (a *codexAdapter) Write(path string, servers map[string]Server) error {
	var existing []byte
	if data, err := os.ReadFile(path); err == nil {
		existing = data
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	stripped := stripMCPBlocks(string(existing))
	stripped = strings.TrimRight(stripped, "\n")

	var out strings.Builder
	if stripped != "" {
		out.WriteString(stripped)
		out.WriteString("\n")
	}
	for _, name := range sortedKeys(servers) {
		s := servers[name]
		if s.IsRemote() {
			fmt.Fprintf(os.Stderr, "syncai: codex stdio-only — skipping remote MCP server %q\n", name)
			continue
		}
		if s.Command == "" {
			continue
		}
		if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n\n") {
			out.WriteString("\n")
		}
		out.WriteString(formatCodexBlock(name, s))
	}

	final := out.String()
	if final != "" && !strings.HasSuffix(final, "\n") {
		final += "\n"
	}
	return util.WriteFile(path, []byte(final))
}

// scanMCPBlocks returns the body of every `[mcp_servers.<name>]` table
// in the document, keyed by <name>. Sub-tables like
// `[mcp_servers.<name>.env]` are merged into their parent's body so
// parseCodexBlock can read the env block.
func scanMCPBlocks(text string) map[string]string {
	blocks := map[string]*strings.Builder{}
	currentName := ""
	currentInEnv := false

	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			header := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if name, ok := strings.CutPrefix(header, "mcp_servers."); ok {
				if dot := strings.IndexByte(name, '.'); dot >= 0 {
					// Sub-table like mcp_servers.foo.env
					parent := name[:dot]
					sub := name[dot+1:]
					if blocks[parent] == nil {
						blocks[parent] = &strings.Builder{}
					}
					currentName = parent
					currentInEnv = sub == "env"
					if currentInEnv {
						blocks[parent].WriteString("__env__:\n")
					}
					continue
				}
				if blocks[name] == nil {
					blocks[name] = &strings.Builder{}
				}
				currentName = name
				currentInEnv = false
				continue
			}
			currentName = ""
			currentInEnv = false
			continue
		}
		if currentName == "" {
			continue
		}
		b := blocks[currentName]
		if currentInEnv {
			b.WriteString("__env_line__:")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	out := make(map[string]string, len(blocks))
	for k, v := range blocks {
		out[k] = v.String()
	}
	return out
}

// stripMCPBlocks removes every `[mcp_servers...]` table (including
// sub-tables) from the document, leaving everything else intact.
func stripMCPBlocks(text string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out strings.Builder
	skipping := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			header := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			skipping = strings.HasPrefix(header, "mcp_servers.") || header == "mcp_servers"
			if skipping {
				continue
			}
		}
		if skipping {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

// parseCodexBlock extracts command/args/env/url from a single MCP
// server block. Only the most common scalar/array/inline-table forms
// are recognized — exotic TOML is treated as opaque.
func parseCodexBlock(body string) Server {
	var s Server
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		// env sub-table line, recorded by scanMCPBlocks
		if strings.HasPrefix(line, "__env_line__:") {
			rest := strings.TrimPrefix(line, "__env_line__:")
			rest = strings.TrimSpace(rest)
			if rest == "" || strings.HasPrefix(rest, "#") {
				continue
			}
			k, v, ok := splitTOMLKV(rest)
			if !ok {
				continue
			}
			if s.Env == nil {
				s.Env = map[string]string{}
			}
			s.Env[k] = parseTOMLString(v)
			continue
		}
		if strings.HasPrefix(line, "__env__:") {
			continue
		}
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		k, v, ok := splitTOMLKV(t)
		if !ok {
			continue
		}
		switch k {
		case "command":
			s.Command = parseTOMLString(v)
		case "args":
			s.Args = parseTOMLStringArray(v)
		case "url":
			s.URL = parseTOMLString(v)
			if s.Type == "" {
				s.Type = "http"
			}
		case "env":
			s.Env = mergeStringMap(s.Env, parseTOMLInlineTable(v))
		}
	}
	return s
}

func formatCodexBlock(name string, s Server) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[mcp_servers.%s]\n", name)
	fmt.Fprintf(&b, "command = %s\n", quoteTOMLString(s.Command))
	if len(s.Args) > 0 {
		b.WriteString("args = ")
		b.WriteString(formatTOMLStringArray(s.Args))
		b.WriteString("\n")
	}
	if len(s.Env) > 0 {
		b.WriteString("env = { ")
		first := true
		for _, k := range sortedStringMapKeys(s.Env) {
			if !first {
				b.WriteString(", ")
			}
			first = false
			fmt.Fprintf(&b, "%s = %s", quoteTOMLKey(k), quoteTOMLString(s.Env[k]))
		}
		b.WriteString(" }\n")
	}
	return b.String()
}

func splitTOMLKV(line string) (string, string, bool) {
	idx := strings.IndexByte(line, '=')
	if idx <= 0 {
		return "", "", false
	}
	k := strings.TrimSpace(line[:idx])
	v := strings.TrimSpace(line[idx+1:])
	if cut := strings.Index(v, " #"); cut >= 0 {
		v = strings.TrimSpace(v[:cut])
	}
	k = strings.Trim(k, "\"'")
	return k, v, k != ""
}

func parseTOMLString(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			inner := v[1 : len(v)-1]
			if v[0] == '"' {
				if s, err := strconv.Unquote(v); err == nil {
					return s
				}
			}
			return inner
		}
	}
	return v
}

func parseTOMLStringArray(v string) []string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
		return nil
	}
	inner := v[1 : len(v)-1]
	var out []string
	var cur strings.Builder
	inStr := false
	var quote byte
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if inStr {
			if c == '\\' && quote == '"' && i+1 < len(inner) {
				cur.WriteByte(c)
				cur.WriteByte(inner[i+1])
				i++
				continue
			}
			if c == quote {
				inStr = false
				continue
			}
			cur.WriteByte(c)
			continue
		}
		if c == '"' || c == '\'' {
			inStr = true
			quote = c
			continue
		}
		if c == ',' {
			s := strings.TrimSpace(cur.String())
			cur.Reset()
			if s != "" {
				out = append(out, unescapeTOMLString(s, quote))
			}
			continue
		}
		// whitespace / other — ignore unless inside string
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, unescapeTOMLString(s, quote))
	}
	// The accumulator above only ran inside-string, but if literal
	// strings appeared they were stripped as we go; keep entries.
	return out
}

func unescapeTOMLString(s string, quote byte) string {
	if quote == '"' {
		if u, err := strconv.Unquote("\"" + s + "\""); err == nil {
			return u
		}
	}
	return s
}

func parseTOMLInlineTable(v string) map[string]string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "{") || !strings.HasSuffix(v, "}") {
		return nil
	}
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" {
		return map[string]string{}
	}
	out := map[string]string{}
	// Naive comma split — sufficient for typical key="value" pairs.
	depth := 0
	inStr := false
	var quote byte
	start := 0
	parts := []string{}
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if inStr {
			if c == '\\' && quote == '"' && i+1 < len(inner) {
				i++
				continue
			}
			if c == quote {
				inStr = false
			}
			continue
		}
		switch c {
		case '"', '\'':
			inStr = true
			quote = c
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, inner[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, inner[start:])
	for _, p := range parts {
		k, val, ok := splitTOMLKV(strings.TrimSpace(p))
		if !ok {
			continue
		}
		out[k] = parseTOMLString(val)
	}
	return out
}

func quoteTOMLString(s string) string {
	return strconv.Quote(s)
}

func quoteTOMLKey(k string) string {
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return strconv.Quote(k)
		}
	}
	return k
}

func formatTOMLStringArray(arr []string) string {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, s := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(quoteTOMLString(s))
	}
	b.WriteByte(']')
	return b.String()
}

func mergeStringMap(a, b map[string]string) map[string]string {
	if a == nil {
		a = map[string]string{}
	}
	for k, v := range b {
		a[k] = v
	}
	return a
}
