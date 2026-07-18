//go:build !linux

package lifecycle

func (m *SystemdManager) removeManagedArtifacts() (bool, error) {
	return false, ErrUnsupportedPlatform
}
