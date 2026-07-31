package openssh

import (
	"bytes"
	"testing"

	sshfx "github.com/pkg/sftp/internal/encoding/ssh/filexfer"
)

var _ sshfx.PacketMarshaller = &StatVFSExtendedPacket{}

func init() {
	RegisterExtensionStatVFS()
}

func TestStatVFSExtendedPacket(t *testing.T) {
	const (
		id   = 42
		path = "/foo"
	)

	ep := &StatVFSExtendedPacket{
		Path: path,
	}

	data, err := sshfx.ComposePacket(ep.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 36,
		200,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 19, 's', 't', 'a', 't', 'v', 'f', 's', '@', 'o', 'p', 'e', 'n', 's', 's', 'h', '.', 'c', 'o', 'm',
		0x00, 0x00, 0x00, 4, '/', 'f', 'o', 'o',
	}

	if !bytes.Equal(data, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", data, want)
	}

	var p sshfx.ExtendedPacket

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(sshfx.NewBuffer(data[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.ExtendedRequest != extensionStatVFS {
		t.Errorf("UnmarshalPacketBody(): ExtendedRequest was %q, but expected %q", p.ExtendedRequest, extensionStatVFS)
	}

	ep, ok := p.Data.(*StatVFSExtendedPacket)
	if !ok {
		t.Fatalf("UnmarshaledPacketBody(): Data was type %T, but expected *StatVFSExtendedPacket", p.Data)
	}

	if ep.Path != path {
		t.Errorf("UnmarshalPacketBody(): Path was %q, but expected %q", ep.Path, path)
	}
}

var _ sshfx.PacketMarshaller = &FStatVFSExtendedPacket{}

func init() {
	RegisterExtensionFStatVFS()
}

func TestFStatVFSExtendedPacket(t *testing.T) {
	const (
		id   = 42
		path = "/foo"
	)

	ep := &FStatVFSExtendedPacket{
		Path: path,
	}

	data, err := sshfx.ComposePacket(ep.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 37,
		200,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 20, 'f', 's', 't', 'a', 't', 'v', 'f', 's', '@', 'o', 'p', 'e', 'n', 's', 's', 'h', '.', 'c', 'o', 'm',
		0x00, 0x00, 0x00, 4, '/', 'f', 'o', 'o',
	}

	if !bytes.Equal(data, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", data, want)
	}

	var p sshfx.ExtendedPacket

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(sshfx.NewBuffer(data[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.ExtendedRequest != extensionFStatVFS {
		t.Errorf("UnmarshalPacketBody(): ExtendedRequest was %q, but expected %q", p.ExtendedRequest, extensionFStatVFS)
	}

	ep, ok := p.Data.(*FStatVFSExtendedPacket)
	if !ok {
		t.Fatalf("UnmarshaledPacketBody(): Data was type %T, but expected *FStatVFSExtendedPacket", p.Data)
	}

	if ep.Path != path {
		t.Errorf("UnmarshalPacketBody(): Path was %q, but expected %q", ep.Path, path)
	}
}

var _ sshfx.Packet = &StatVFSExtendedReplyPacket{}

func TestStatVFSExtendedReplyPacket(t *testing.T) {
	const (
		id   = 42
		path = "/foo"
	)

	const (
		BlockSize = uint64(iota + 13)
		FragmentSize
		Blocks
		BlocksFree
		BlocksAvail
		Files
		FilesFree
		FilesAvail
		FilesystemID
		MountFlags
		MaxNameLength
	)

	ep := &StatVFSExtendedReplyPacket{
		BlockSize:     BlockSize,
		FragmentSize:  FragmentSize,
		Blocks:        Blocks,
		BlocksFree:    BlocksFree,
		BlocksAvail:   BlocksAvail,
		Files:         Files,
		FilesFree:     FilesFree,
		FilesAvail:    FilesAvail,
		FilesystemID:  FilesystemID,
		MountFlags:    MountFlags,
		MaxNameLength: MaxNameLength,
	}

	data, err := sshfx.ComposePacket(ep.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 93,
		201,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 13,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 14,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 15,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 16,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 17,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 18,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 19,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 20,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 21,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 22,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 23,
	}

	if !bytes.Equal(data, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", data, want)
	}

	*ep = StatVFSExtendedReplyPacket{}

	p := sshfx.ExtendedReplyPacket{
		Data: ep,
	}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(sshfx.NewBuffer(data[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	ep, ok := p.Data.(*StatVFSExtendedReplyPacket)
	if !ok {
		t.Fatalf("UnmarshaledPacketBody(): Data was type %T, but expected *StatVFSExtendedReplyPacket", p.Data)
	}

	if ep.BlockSize != BlockSize {
		t.Errorf("UnmarshalPacketBody(): BlockSize was %d, but expected %d", ep.BlockSize, BlockSize)
	}

	if ep.FragmentSize != FragmentSize {
		t.Errorf("UnmarshalPacketBody(): FragmentSize was %d, but expected %d", ep.FragmentSize, FragmentSize)
	}

	if ep.Blocks != Blocks {
		t.Errorf("UnmarshalPacketBody(): Blocks was %d, but expected %d", ep.Blocks, Blocks)
	}

	if ep.BlocksFree != BlocksFree {
		t.Errorf("UnmarshalPacketBody(): BlocksFree was %d, but expected %d", ep.BlocksFree, BlocksFree)
	}

	if ep.BlocksAvail != BlocksAvail {
		t.Errorf("UnmarshalPacketBody(): BlocksAvail was %d, but expected %d", ep.BlocksAvail, BlocksAvail)
	}

	if ep.Files != Files {
		t.Errorf("UnmarshalPacketBody(): Files was %d, but expected %d", ep.Files, Files)
	}

	if ep.FilesFree != FilesFree {
		t.Errorf("UnmarshalPacketBody(): FilesFree was %d, but expected %d", ep.FilesFree, FilesFree)
	}

	if ep.FilesAvail != FilesAvail {
		t.Errorf("UnmarshalPacketBody(): FilesAvail was %d, but expected %d", ep.FilesAvail, FilesAvail)
	}

	if ep.FilesystemID != FilesystemID {
		t.Errorf("UnmarshalPacketBody(): FilesystemID was %d, but expected %d", ep.FilesystemID, FilesystemID)
	}

	if ep.MountFlags != MountFlags {
		t.Errorf("UnmarshalPacketBody(): MountFlags was %d, but expected %d", ep.MountFlags, MountFlags)
	}

	if ep.MaxNameLength != MaxNameLength {
		t.Errorf("UnmarshalPacketBody(): MaxNameLength was %d, but expected %d", ep.MaxNameLength, MaxNameLength)
	}
}
