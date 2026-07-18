//go:build !linux

package lifecycle

type trustedUnitRemoval struct{}

func (m *SystemdManager) prepareUnitRemoval(fileIdentity) (*trustedUnitRemoval, error) {
	return nil, ErrUnsupportedPlatform
}

func (removal *trustedUnitRemoval) Remove() error { return ErrUnsupportedPlatform }
func (removal *trustedUnitRemoval) Close() error  { return nil }
