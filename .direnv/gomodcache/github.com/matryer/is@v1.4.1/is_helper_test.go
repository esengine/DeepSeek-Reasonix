// +build go1.7

package is

func helper(is *I, helper bool) {
	if helper {
		is.Helper()
	}
	is.True(false)
}
