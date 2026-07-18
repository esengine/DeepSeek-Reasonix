//go:build !linux

package cli

// V1 Host service support remains Linux-only. Non-Linux callers continue into
// service.DefaultEndpoint, which reports the established unsupported-platform
// boundary instead of pretending to apply a Unix effective-uid policy.
func productionRemoteServePrivilegeGuard() error { return nil }
