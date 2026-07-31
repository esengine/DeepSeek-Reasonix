package openssh

import (
	"bytes"
	"testing"

	sshfx "github.com/pkg/sftp/internal/encoding/ssh/filexfer"
)

var _ sshfx.PacketMarshaller = &HardlinkExtendedPacket{}

func init() {
	RegisterExtensionHardlink()
}

func TestHardlinkExtendedPacket(t *testing.T) {
	const (
		id      = 42
		oldpath = "/foo"
		newpath = "/bar"
	)

	ep := &HardlinkExtendedPacket{
		OldPath: oldpath,
		NewPath: newpath,
	}

	data, err := sshfx.ComposePacket(ep.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 45,
		200,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 20, 'h', 'a', 'r', 'd', 'l', 'i', 'n', 'k', '@', 'o', 'p', 'e', 'n', 's', 's', 'h', '.', 'c', 'o', 'm',
		0x00, 0x00, 0x00, 4, '/', 'f', 'o', 'o',
		0x00, 0x00, 0x00, 4, '/', 'b', 'a', 'r',
	}

	if !bytes.Equal(data, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", data, want)
	}

	var p sshfx.ExtendedPacket

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(sshfx.NewBuffer(data[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.ExtendedRequest != extensionHardlink {
		t.Errorf("UnmarshalPacketBody(): ExtendedRequest was %q, but expected %q", p.ExtendedRequest, extensionHardlink)
	}

	ep, ok := p.Data.(*HardlinkExtendedPacket)
	if !ok {
		t.Fatalf("UnmarshaledPacketBody(): Data was type %T, but expected *HardlinkExtendedPacket", p.Data)
	}

	if ep.OldPath != oldpath {
		t.Errorf("UnmarshalPacketBody(): OldPath was %q, but expected %q", ep.OldPath, oldpath)
	}

	if ep.NewPath != newpath {
		t.Errorf("UnmarshalPacketBody(): NewPath was %q, but expected %q", ep.NewPath, newpath)
	}
}
