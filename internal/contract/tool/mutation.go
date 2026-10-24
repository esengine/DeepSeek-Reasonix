package tool

import (
	"context"
	"encoding/json"
)

type MutationKind string

const (
	MutationReadOnly    MutationKind = "read_only"
	MutationNamedPaths  MutationKind = "named_paths"
	MutationWorkspace   MutationKind = "workspace"
	MutationGitMetadata MutationKind = "git_metadata"
	MutationExternal    MutationKind = "external"
)

// MutationImpact describes execution coordination, independently of permission
// and evidence. SharedResources are host metadata, not added write authority.
type MutationImpact struct {
	Kind            MutationKind
	Paths           []string
	SharedResources []string
}

type MutationDescriber interface {
	DescribeMutation(context.Context, json.RawMessage) (MutationImpact, error)
}
