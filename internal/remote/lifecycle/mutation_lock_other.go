//go:build !linux

package lifecycle

import "context"

func (m *SystemdManager) acquireMutationLock(context.Context) (mutationLock, error) {
	return nil, ErrUnsupportedPlatform
}
