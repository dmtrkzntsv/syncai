package syncai

import (
	"path/filepath"
	"strings"
	"syncai/internal/config"
	"syncai/internal/model"
)

func (s *SyncAI) generatePath(agent *config.Agent, kind model.Kind, stem, rel string) string {
	if agent == nil {
		return ""
	}
	switch kind {
	case model.KindContext:
		return agent.Context.Path
	case model.KindIgnore:
		return agent.Ignore.Path
	case model.KindRules:
		return generatePatternPath(agent.Rules.Pattern, stem)
	case model.KindSkills:
		dir := generatePatternPath(agent.Skills.Pattern, stem)
		if dir == "" {
			return ""
		}
		if rel == "" {
			return dir
		}
		return filepath.Join(dir, rel)
	case model.KindMCP:
		return agent.MCP.Path
	default:
		return ""
	}
}

// generatePatternPath substitutes the stem into every "*" component in the
// pattern. A pattern with no "*" is returned unchanged.
func generatePatternPath(pattern, stem string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}
	if !strings.Contains(pattern, "*") {
		return pattern
	}
	parts := strings.Split(filepath.ToSlash(pattern), "/")
	for i, c := range parts {
		if strings.Contains(c, "*") {
			parts[i] = strings.ReplaceAll(c, "*", stem)
		}
	}
	return filepath.FromSlash(strings.Join(parts, "/"))
}

// matchPattern matches a file path against a glob-like pattern that may
// contain "*" in any component. Returns the captured stem on success. For
// patterns with no "*", the basename of path without its extension is
// returned as the stem (legacy rules behavior).
func matchPattern(pattern, path string) (bool, string) {
	if strings.TrimSpace(pattern) == "" {
		return false, ""
	}
	pNorm := filepath.ToSlash(filepath.Clean(pattern))
	fNorm := filepath.ToSlash(filepath.Clean(path))
	pParts := strings.Split(pNorm, "/")
	fParts := strings.Split(fNorm, "/")
	if len(pParts) != len(fParts) {
		return false, ""
	}
	hasWildcard := strings.Contains(pNorm, "*")
	var stem string
	var stemSet bool
	for i := range pParts {
		pc, fc := pParts[i], fParts[i]
		if strings.Contains(pc, "*") {
			parts := strings.SplitN(pc, "*", 2)
			prefix, suffix := parts[0], ""
			if len(parts) > 1 {
				suffix = parts[1]
			}
			if len(fc) < len(prefix)+len(suffix) ||
				!strings.HasPrefix(fc, prefix) ||
				!strings.HasSuffix(fc, suffix) {
				return false, ""
			}
			captured := fc[len(prefix) : len(fc)-len(suffix)]
			if stemSet && captured != stem {
				return false, ""
			}
			stem = captured
			stemSet = true
		} else if pc != fc {
			return false, ""
		}
	}
	if !hasWildcard {
		base := filepath.Base(path)
		if ext := filepath.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		stem = base
	}
	return true, stem
}

// matchSkillsPattern checks whether a file path lies inside a skill directory
// matched by pattern. The pattern is treated as a directory glob; the file
// path may have additional components after the matched directory. Returns
// (matched, stem, rel) where stem is the captured wildcard text (skill folder
// name) and rel is the file's path relative to the skill directory.
func matchSkillsPattern(pattern, path string) (bool, string, string) {
	if strings.TrimSpace(pattern) == "" {
		return false, "", ""
	}
	pNorm := filepath.ToSlash(filepath.Clean(pattern))
	fNorm := filepath.ToSlash(filepath.Clean(path))
	pParts := strings.Split(pNorm, "/")
	fParts := strings.Split(fNorm, "/")
	if len(fParts) < len(pParts) {
		return false, "", ""
	}
	var stem string
	var stemSet bool
	for i := range pParts {
		pc, fc := pParts[i], fParts[i]
		if strings.Contains(pc, "*") {
			parts := strings.SplitN(pc, "*", 2)
			prefix, suffix := parts[0], ""
			if len(parts) > 1 {
				suffix = parts[1]
			}
			if len(fc) < len(prefix)+len(suffix) ||
				!strings.HasPrefix(fc, prefix) ||
				!strings.HasSuffix(fc, suffix) {
				return false, "", ""
			}
			captured := fc[len(prefix) : len(fc)-len(suffix)]
			if stemSet && captured != stem {
				return false, "", ""
			}
			stem = captured
			stemSet = true
		} else if pc != fc {
			return false, "", ""
		}
	}
	rel := strings.Join(fParts[len(pParts):], "/")
	return true, stem, rel
}

// skillsBaseDir returns the prefix of pattern up to (but not including) its
// first wildcard component. For ".codex/skills/*" it returns ".codex/skills".
// For patterns with no wildcard it returns the pattern's parent dir.
func skillsBaseDir(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}
	pNorm := filepath.ToSlash(filepath.Clean(pattern))
	parts := strings.Split(pNorm, "/")
	for i, c := range parts {
		if strings.Contains(c, "*") {
			if i == 0 {
				return ""
			}
			return filepath.FromSlash(strings.Join(parts[:i], "/"))
		}
	}
	return filepath.Dir(filepath.FromSlash(pNorm))
}
