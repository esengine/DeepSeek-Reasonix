//go:build !windows

package sysproxy

import "net/url"

// ForURL has no OS proxy source outside Windows; env/direct handling stays with
// the caller. The unused list/bypass helpers keep one cross-platform file.
func ForURL(*url.URL) (*url.URL, error) {
	_ = parseProxyList
	_ = bypassed
	return nil, nil
}
