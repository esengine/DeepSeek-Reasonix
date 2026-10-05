//go:build !darwin

package egress

// systemProxies has no OS proxy source to read here; the environment is the
// only one LoopbackProxyPorts consults.
func systemProxies() []string { return nil }
