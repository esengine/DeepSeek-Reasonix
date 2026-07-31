package openssh

import (
	"bytes"
	"testing"

	sshfx "github.com/pkg/sftp/internal/encoding/ssh/filexfer"
)

var _ sshfx.PacketMarshaller = &FSyncExtendedPacket{}

func init() {
	RegisterExtensionFSync()
}

func TestFSyncExtendedPacket(t *testing.T) {
	const (
		id     = 42
		handle = "somehandle"
	)

	ep := &FSyncExtendedPacket{
		Handle: handle,
	}

	data, err := sshfx.ComposePacket(ep.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 40,
		200,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 17, 'f', 's', 'y', 'n', 'c', '@', 'o', 'p', 'e', 'n', 's', 's', 'h', '.', 'c', 'o', 'm',
		0x00, 0x00, 0x00, 10, 's', 'o', 'm', 'e', 'h', 'a', 'n', 'd', 'l', 'e',
	}

	if !bytes.Equal(data, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", data, want)
	}

	var p sshfx.ExtendedPacket

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(sshfx.NewBuffer(data[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.ExtendedRequest != extensionFSync {
		t.Errorf("UnmarshalPacketBody(): ExtendedRequest was %q, but expected %q", p.ExtendedRequest, extensionFSync)
	}

	ep, ok := p.Data.(*FSyncExtendedPacket)
	if !ok {
		t.Fatalf("UnmarshaledPacketBody(): Data was type %T, but expected *FSyncExtendedPacket", p.Data)
	}

	if ep.Handle != handle {
		t.Errorf("UnmarshalPacketBody(): Handle was %q, but expected %q", ep.Handle, handle)
	}
}
