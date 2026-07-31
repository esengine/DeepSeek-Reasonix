package sshfx

import (
	"bytes"
	"testing"
)

var _ Packet = &ClosePacket{}

func TestClosePacket(t *testing.T) {
	const (
		id     = 42
		handle = "somehandle"
	)

	p := &ClosePacket{
		Handle: "somehandle",
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 19,
		4,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 10, 's', 'o', 'm', 'e', 'h', 'a', 'n', 'd', 'l', 'e',
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = ClosePacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Handle != handle {
		t.Errorf("UnmarshalPacketBody(): Handle was %q, but expected %q", p.Handle, handle)
	}
}

var _ Packet = &ReadPacket{}

func TestReadPacket(t *testing.T) {
	const (
		id            = 42
		handle        = "somehandle"
		offset uint64 = 0x123456789ABCDEF0
		length uint32 = 0xFEDCBA98
	)

	p := &ReadPacket{
		Handle: "somehandle",
		Offset: offset,
		Length: length,
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 31,
		5,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 10, 's', 'o', 'm', 'e', 'h', 'a', 'n', 'd', 'l', 'e',
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0,
		0xFE, 0xDC, 0xBA, 0x98,
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = ReadPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Handle != handle {
		t.Errorf("UnmarshalPacketBody(): Handle was %q, but expected %q", p.Handle, handle)
	}

	if p.Offset != offset {
		t.Errorf("UnmarshalPacketBody(): Offset was %x, but expected %x", p.Offset, offset)
	}

	if p.Length != length {
		t.Errorf("UnmarshalPacketBody(): Length was %x, but expected %x", p.Length, length)
	}
}

var _ Packet = &WritePacket{}

func TestWritePacket(t *testing.T) {
	const (
		id            = 42
		handle        = "somehandle"
		offset uint64 = 0x123456789ABCDEF0
	)

	var payload = []byte(`foobar`)

	p := &WritePacket{
		Handle: "somehandle",
		Offset: offset,
		Data:   payload,
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 37,
		6,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 10, 's', 'o', 'm', 'e', 'h', 'a', 'n', 'd', 'l', 'e',
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0,
		0x00, 0x00, 0x00, 0x06, 'f', 'o', 'o', 'b', 'a', 'r',
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = WritePacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Handle != handle {
		t.Errorf("UnmarshalPacketBody(): Handle was %q, but expected %q", p.Handle, handle)
	}

	if p.Offset != offset {
		t.Errorf("UnmarshalPacketBody(): Offset was %x, but expected %x", p.Offset, offset)
	}

	if !bytes.Equal(p.Data, payload) {
		t.Errorf("UnmarshalPacketBody(): Data was %X, but expected %X", p.Data, payload)
	}
}

var _ Packet = &FStatPacket{}

func TestFStatPacket(t *testing.T) {
	const (
		id     = 42
		handle = "somehandle"
	)

	p := &FStatPacket{
		Handle: "somehandle",
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 19,
		8,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 10, 's', 'o', 'm', 'e', 'h', 'a', 'n', 'd', 'l', 'e',
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = FStatPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Handle != handle {
		t.Errorf("UnmarshalPacketBody(): Handle was %q, but expected %q", p.Handle, handle)
	}
}

var _ Packet = &FSetstatPacket{}

func TestFSetstatPacket(t *testing.T) {
	const (
		id     = 42
		handle = "somehandle"
		perms  = 0x87654321
	)

	p := &FSetstatPacket{
		Handle: "somehandle",
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
		0x00, 0x00, 0x00, 27,
		10,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 10, 's', 'o', 'm', 'e', 'h', 'a', 'n', 'd', 'l', 'e',
		0x00, 0x00, 0x00, 0x04,
		0x87, 0x65, 0x43, 0x21,
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = FSetstatPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Handle != handle {
		t.Errorf("UnmarshalPacketBody(): Handle was %q, but expected %q", p.Handle, handle)
	}
}

var _ Packet = &ReadDirPacket{}

func TestReadDirPacket(t *testing.T) {
	const (
		id     = 42
		handle = "somehandle"
	)

	p := &ReadDirPacket{
		Handle: "somehandle",
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 19,
		12,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 10, 's', 'o', 'm', 'e', 'h', 'a', 'n', 'd', 'l', 'e',
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = ReadDirPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Handle != handle {
		t.Errorf("UnmarshalPacketBody(): Handle was %q, but expected %q", p.Handle, handle)
	}
}
