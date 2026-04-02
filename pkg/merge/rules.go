package merge

import "github.com/odvcencio/graft/pkg/entity"

// DiagSeverity indicates the severity of a merge rule diagnostic.
type DiagSeverity int

const (
	DiagInfo DiagSeverity = iota
	DiagWarning
	DiagError
)

func (s DiagSeverity) String() string {
	switch s {
	case DiagInfo:
		return "info"
	case DiagWarning:
		return "warning"
	case DiagError:
		return "error"
	default:
		return "unknown"
	}
}

// Diagnostic is a message produced by a post-merge rule.
type Diagnostic struct {
	Severity DiagSeverity
	Entity   string // Entity identity key
	Message  string
	Rule     string // Rule identifier
}

// MergeRuleContext provides the merge state to rules.
type MergeRuleContext struct {
	Base     []entity.Entity
	Ours     []entity.Entity
	Theirs   []entity.Entity
	Matched  []MatchedEntity
	Result   *MergeResult
	Language string
	Path     string
}

// LangMergeRule inspects a merge result and returns diagnostics.
// Rules are advisory-only: they produce diagnostics but do not
// mutate the merge output.
type LangMergeRule interface {
	Language() string
	Apply(ctx *MergeRuleContext) []Diagnostic
}

// RuleRegistry holds registered merge rules by language.
type RuleRegistry struct {
	rules map[string][]LangMergeRule
}

// NewRuleRegistry creates an empty rule registry.
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{rules: make(map[string][]LangMergeRule)}
}

// Register adds a rule to the registry.
func (r *RuleRegistry) Register(rule LangMergeRule) {
	lang := rule.Language()
	r.rules[lang] = append(r.rules[lang], rule)
}

// RulesFor returns all rules registered for the given language.
func (r *RuleRegistry) RulesFor(lang string) []LangMergeRule {
	return r.rules[lang]
}

// DefaultRegistry is the global rule registry used by MergeFiles.
var DefaultRegistry = NewRuleRegistry()
