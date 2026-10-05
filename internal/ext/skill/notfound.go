// notfound.go — the answer to a skill name nothing answers to, as an identity
// rather than a sentence: a caller that has to tell it from a store that would
// not open is left matching words otherwise.
package skill

import "fmt"

// NotFoundError names a skill this session does not carry. It holds the name
// because the sentence a frontend renders is built from it, never parsed back
// out of this one.
type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("unknown skill: %s", e.Name) }
