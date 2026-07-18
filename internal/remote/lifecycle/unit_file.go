package lifecycle

type fileIdentity struct {
	Device uint64
	Inode  uint64
}

func (identity fileIdentity) matches(device, inode uint64) bool {
	return identity.Device == device && identity.Inode == inode && identity.Inode != 0
}
