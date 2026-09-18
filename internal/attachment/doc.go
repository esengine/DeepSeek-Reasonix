// Package attachment validates, persists, and serves session image objects.
//
// Bytes live in sessioncontent. Session authorization and Desktop identity
// stay in their owners. This package never infers a store root from process
// cwd, the focused tab, or an attachment id.
package attachment
