package sshfx

import (
	"bytes"
	"testing"
)

var _ Packet = &OpenPacket{}

func TestOpenPacket(t *testing.T) {
	const (
		id                = 42
		filename          = "/foo"
		perms    FileMode = 0x87654321
	)

	p := &OpenPacket{
		Filename: "/foo",
		PFlags:   FlagRead,
		Attrs: Attributes{
			Flags:       AttrPermissions,
			Permissions: perms,
		},
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 25,
		3,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 4, '/', 'f', 'o', 'o',
		0x00, 0x00, 0x00, 1,
		0x00, 0x00, 0x00, 0x04,
		0x87, 0x65, 0x43, 0x21,
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = OpenPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Filename != filename {
		t.Errorf("UnmarshalPacketBody(): Filename was %q, but expected %q", p.Filename, filename)
	}

	if p.PFlags != FlagRead {
		t.Errorf("UnmarshalPacketBody(): PFlags was %#x, but expected %#x", p.PFlags, FlagRead)
	}

	if p.Attrs.Flags != AttrPermissions {
		t.Errorf("UnmarshalPacketBody(): Attrs.Flags was %#x, but expected %#x", p.Attrs.Flags, AttrPermissions)
	}

	if p.Attrs.Permissions != perms {
		t.Errorf("UnmarshalPacketBody(): Attrs.Permissions was %#v, but expected %#v", p.Attrs.Permissions, perms)
	}
}

var _ Packet = &OpenDirPacket{}

func TestOpenDirPacket(t *testing.T) {
	const (
		id   = 42
		path = "/foo"
	)

	p := &OpenDirPacket{
		Path: path,
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 13,
		11,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 4, '/', 'f', 'o', 'o',
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = OpenDirPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Path != path {
		t.Errorf("UnmarshalPacketBody(): Path was %q, but expected %q", p.Path, path)
	}
}
