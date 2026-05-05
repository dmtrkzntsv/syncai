package syncai

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syncai/internal/generator"
	"syncai/internal/model"
	"syncai/internal/util"

	"syncai/internal/config"
)

type SyncAI struct {
	cfg config.Config
}

func New(cfg config.Config) *SyncAI {
	return &SyncAI{cfg: cfg}
}

// Delete propagates deletion of a watched file to corresponding destinations across other agents.
func (s *SyncAI) Delete(path string) ([]string, error) {
	result := make([]string, 0)
	srcAgent, kind, stem, rel := s.Identify(path)
	result = append(result, path)
	if kind == model.KindUnknown || srcAgent == nil {
		return result, nil // nothing to do
	}

	// Only propagate deletions for rules and skills. Context/ignore deletions
	// are not propagated to avoid accidental removals.
	if kind != model.KindRules && kind != model.KindSkills {
		return result, nil
	}

	for i := range s.cfg.Agents {
		dstAgent := &s.cfg.Agents[i]
		if srcAgent.Name == dstAgent.Name {
			continue
		}

		dstPath := s.generatePath(dstAgent, kind, stem, rel)
		if dstPath == "" {
			continue
		}
		if err := os.Remove(dstPath); err != nil {
			if os.IsNotExist(err) {
				// Already gone at the destination
				result = append(result, dstPath)
			} else {
				return result, err
			}
		} else {
			result = append(result, dstPath)
		}

		// For skills, prune now-empty parent dirs up to the skills base.
		if kind == model.KindSkills {
			util.PruneEmptyDirs(filepath.Dir(dstPath), skillsBaseDir(dstAgent.Skills.Pattern))
		}
	}
	return result, nil
}

// Sync propagates creation/update of a watched file across other agents.
func (s *SyncAI) Sync(path string) ([]string, error) {
	result := make([]string, 0)
	srcAgent, kind, stem, rel := s.Identify(path)
	if kind == model.KindUnknown || srcAgent == nil {
		return result, nil // unknown file, ignore
	}

	if kind == model.KindSkills {
		return s.syncSkill(srcAgent, path, stem, rel)
	}

	stack := model.DocumentStack{
		Documents:   make([]model.Document, 0),
		ChangedPath: path,
		Properties: model.Properties{
			Kind: kind,
			Stem: stem,
		},
	}
	for i := range s.cfg.Agents {
		dstAgent := &s.cfg.Agents[i]

		var docPath string
		if dstAgent.Name == srcAgent.Name {
			docPath = path
		} else {
			docPath = s.generatePath(dstAgent, kind, stem, rel)
			if docPath == "" {
				continue
			}
		}
		if util.IsFileExists(docPath) {
			doc, err := util.ParseFile(docPath)
			if err != nil {
				return result, fmt.Errorf("parse %s for agent %s: %w", docPath, dstAgent.Name, err)
			}
			stack.Push(doc)
		}
	}

	for i := range s.cfg.Agents {
		dstAgent := &s.cfg.Agents[i]
		if srcAgent.Name == dstAgent.Name {
			continue
		}

		dstPath := s.generatePath(dstAgent, kind, stem, rel)
		if strings.TrimSpace(dstPath) == "" {
			// No target path configured for this agent/kind; skip writing
			continue
		}
		data, err := generate(&stack, dstAgent.Name)
		if err != nil {
			return result, fmt.Errorf("generate stack for agent %s: %w", dstAgent.Name, err)
		}
		if err = util.WriteFile(dstPath, data); err != nil {
			return result, fmt.Errorf("write %s for agent %s: %w", dstPath, dstAgent.Name, err)
		}
		result = append(result, dstPath)
		log.Printf("File %s synced to %s", path, dstPath)
	}

	return result, nil
}

// syncSkill copies a single file inside a skill folder verbatim to every
// other agent that has a skills pattern configured.
func (s *SyncAI) syncSkill(srcAgent *config.Agent, path, stem, rel string) ([]string, error) {
	result := make([]string, 0)
	data, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("read %s: %w", path, err)
	}

	for i := range s.cfg.Agents {
		dstAgent := &s.cfg.Agents[i]
		if srcAgent.Name == dstAgent.Name {
			continue
		}
		if strings.TrimSpace(dstAgent.Skills.Pattern) == "" {
			continue
		}
		dstPath := s.generatePath(dstAgent, model.KindSkills, stem, rel)
		if strings.TrimSpace(dstPath) == "" {
			continue
		}
		if err := util.WriteFile(dstPath, data); err != nil {
			return result, fmt.Errorf("write %s for agent %s: %w", dstPath, dstAgent.Name, err)
		}
		result = append(result, dstPath)
		log.Printf("Skill file %s synced to %s", path, dstPath)
	}
	return result, nil
}

func (s *SyncAI) Identify(path string) (*config.Agent, model.Kind, string, string) {
	clean := filepath.Clean(path)
	for i := range s.cfg.Agents {
		a := &s.cfg.Agents[i]
		if p := strings.TrimSpace(a.Context.Path); p != "" && filepath.Clean(p) == clean {
			return a, model.KindContext, "", ""
		}
		if p := strings.TrimSpace(a.Ignore.Path); p != "" && filepath.Clean(p) == clean {
			return a, model.KindIgnore, "", ""
		}
		if pat := strings.TrimSpace(a.Rules.Pattern); pat != "" {
			if ok, stem := matchPattern(pat, clean); ok {
				return a, model.KindRules, stem, ""
			}
		}
		if pat := strings.TrimSpace(a.Skills.Pattern); pat != "" {
			if ok, stem, rel := matchSkillsPattern(pat, clean); ok {
				return a, model.KindSkills, stem, rel
			}
		}
	}
	return nil, model.KindUnknown, "", ""
}

func generate(s *model.DocumentStack, agent string) ([]byte, error) {
	// Sort documents by ModTime.
	// The document with ChangedPath is always considered the "newest" and placed last,
	// regardless of its actual modification time. This ensures that the changed document
	// is prioritized for further processing, even if its ModTime is older than others.
	sort.Slice(s.Documents, func(i, j int) bool {
		if s.Documents[i].FileInfo.Path == s.ChangedPath {
			return false
		}
		if s.Documents[j].FileInfo.Path == s.ChangedPath {
			return true
		}
		return s.Documents[i].FileInfo.ModTime.Before(s.Documents[j].FileInfo.ModTime)
	})

	if len(s.Documents) == 0 {
		return []byte{}, fmt.Errorf("no documents in stack")
	}
	newestDoc := s.Documents[len(s.Documents)-1]
	content := newestDoc.Content
	agentName := strings.ToLower(agent)

	if s.Properties.Kind == model.KindRules {
		if gen := generator.GetRulesGenerator(agentName); gen != nil {
			metadata := generator.ExtractRulesMetadata(s)
			content = gen.GenerateRules(metadata, content)
		}
	}

	// Return content of newest file
	return content, nil
}
