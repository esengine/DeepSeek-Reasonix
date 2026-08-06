package extension

import "context"

// Contributor offers contributions to the kernel. Implementations wrap one
// discovery source (built-in tools, a skill store, command roots, ...) and
// return everything that source currently provides. Contribute must be
// deterministic for a fixed underlying state: the Builder stamps per-
// contributor registration order, and determinism guarantees that two builds
// over the same state produce the same snapshot.
type Contributor interface {
	// Name identifies the contributor in diagnostics and default Origins.
	Name() string
	// Contribute returns the contributor's current contributions.
	Contribute(ctx context.Context) ([]Contribution, error)
}

// ContributorFunc adapts a function into a Contributor. The name is explicit
// because "the closure passed at some call site" is useless in a conflict
// report.
type ContributorFunc struct {
	ContributorName string
	Fn              func(ctx context.Context) ([]Contribution, error)
}

// Name returns the declared contributor name.
func (f ContributorFunc) Name() string { return f.ContributorName }

// Contribute invokes the wrapped function.
func (f ContributorFunc) Contribute(ctx context.Context) ([]Contribution, error) {
	return f.Fn(ctx)
}
