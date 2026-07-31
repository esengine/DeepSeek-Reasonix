package sftp

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestPflags(t *testing.T) {
	pflags := newFileOpenFlags(sshFxfRead | sshFxfWrite | sshFxfAppend)
	assert.True(t, pflags.Read)
	assert.True(t, pflags.Write)
	assert.True(t, pflags.Append)
	assert.False(t, pflags.Creat)
	assert.False(t, pflags.Trunc)
	assert.False(t, pflags.Excl)
}

func TestRequestAflags(t *testing.T) {
	aflags := newFileAttrFlags(
		sshFileXferAttrSize | sshFileXferAttrUIDGID)
	assert.True(t, aflags.Size)
	assert.True(t, aflags.UidGid)
	assert.False(t, aflags.Acmodtime)
	assert.False(t, aflags.Permissions)
}

func TestRequestAttributes(t *testing.T) {
	// UID/GID
	fa := FileStat{UID: 1, GID: 2}
	fl := uint32(sshFileXferAttrUIDGID)
	at := []byte{}
	at = marshalUint32(at, 1)
	at = marshalUint32(at, 2)
	testFs, _, err := unmarshalFileStat(fl, at)
	require.NoError(t, err)
	assert.Equal(t, fa, *testFs)
	// Size and Mode
	fa = FileStat{Mode: 0700, Size: 99}
	fl = uint32(sshFileXferAttrSize | sshFileXferAttrPermissions)
	at = []byte{}
	at = marshalUint64(at, 99)
	at = marshalUint32(at, 0700)
	testFs, _, err = unmarshalFileStat(fl, at)
	require.NoError(t, err)
	assert.Equal(t, fa, *testFs)
	// FileMode
	assert.True(t, testFs.FileMode().IsRegular())
	assert.False(t, testFs.FileMode().IsDir())
	assert.Equal(t, testFs.FileMode().Perm(), os.FileMode(0700).Perm())
}

func TestRequestAttributesEmpty(t *testing.T) {
	fs, b, err := unmarshalFileStat(sshFileXferAttrAll, []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // size
		0x00, 0x00, 0x00, 0x00, // mode
		0x00, 0x00, 0x00, 0x00, // mtime
		0x00, 0x00, 0x00, 0x00, // atime
		0x00, 0x00, 0x00, 0x00, // uid
		0x00, 0x00, 0x00, 0x00, // gid
		0x00, 0x00, 0x00, 0x00, // extended_count
	})
	require.NoError(t, err)
	assert.Equal(t, &FileStat{
		Extended: []StatExtended{},
	}, fs)
	assert.Empty(t, b)
}
