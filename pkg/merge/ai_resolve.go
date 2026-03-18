package merge

import (
	"context"
)

// AIResolver resolves entity-level merge conflicts using an external AI service.
// Implementations live outside of Graft (e.g. in Orchard).
type AIResolver interface {
	Resolve(ctx context.Context, req AIResolveRequest) (*AIResolveResult, error)
}

// AIResolveRequest contains the entity-level context for an AI merge resolution.
type AIResolveRequest struct {
	FilePath   string
	EntityKey  string
	EntityKind string
	Language   string
	BaseBody   []byte
	OursBody   []byte
	TheirsBody []byte
}

// AIResolveResult holds the AI-generated resolution for a merge conflict.
type AIResolveResult struct {
	ResolvedBody []byte
	Explanation  string
	Confidence   float64
}
