// Package secretstest registers values for masking from tests, through the
// same watched source a store uses.
package secretstest

import "reasonix/internal/base/secrets"

// Hold masks value under label for the rest of the test binary, as a store
// answering for it would; nothing forgets a value once it has been held.
func Hold(label, value string) {
	secrets.WatchValues("secretstest:"+label+"\x00"+value, func() []secrets.Labeled {
		return []secrets.Labeled{{Label: label, Value: value}}
	})
}
