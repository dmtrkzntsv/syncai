package model

import (
	"time"
)

type Kind string

const (
	KindUnknown Kind = ""
	KindRules   Kind = "rules"
	KindContext Kind = "context"
	KindIgnore  Kind = "ignore"
	KindSkills  Kind = "skills"
)

const (
	AgentCursor  string = "cursor"
	AgentCopilot string = "copilot"
)

type FileInfo struct {
	Path    string
	ModTime time.Time
}

type DocumentMetadata struct {
	Raw map[string]string
}

type Document struct {
	FileInfo FileInfo
	Metadata DocumentMetadata
	Content  []byte
}

type RulesMetadata struct {
	Description string
	Globs       string
	ExtraFields map[string]string
}

func (m *RulesMetadata) IsAlwaysApply() bool {
	return m.Globs == "**" || m.Globs == "*"
}
