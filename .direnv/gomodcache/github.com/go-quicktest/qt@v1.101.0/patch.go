// Licensed under the MIT license, see LICENSE file for details.

package qt

import "testing"

// Patch sets a variable to a temporary value for the duration of the test.
//
// It sets the value pointed to by the given destination to the given
// value, which must be assignable to the element type of the destination.
//
// At the end of the test (see "Deferred execution" in the package docs), the
// destination is set back to its original value.
func Patch[T any](tb testing.TB, dest *T, value T) {
	old := *dest
	*dest = value
	tb.Cleanup(func() {
		*dest = old
	})
}
