// authority.go — what the host lets a worker's execution prove, as opposed to
// what that worker owes. The two are declared apart because conflating them
// made the report's own label the capability: a worker contracted to deliver
// one kind could assert the other and close an obligation it was never granted.
package skill

import "slices"

// AuthorityContract is what a worker's execution is allowed to establish. It is
// a grant, not a format: producing a security-shaped report and being permitted
// to satisfy the security obligation are different permissions, and only the
// second one belongs here.
type AuthorityContract struct {
	// Satisfies are the review obligations a report from this worker may close,
	// empty when it may close none.
	Satisfies []string
}

// GrantsReview reports whether this contract may close the named obligation.
func (a AuthorityContract) GrantsReview(kind string) bool {
	return slices.Contains(a.Satisfies, kind)
}
