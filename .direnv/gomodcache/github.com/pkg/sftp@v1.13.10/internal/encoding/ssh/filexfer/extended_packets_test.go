package sshfx

import (
	"bytes"
	"testing"
)

type testExtendedData struct {
	value uint8
}

func (d *testExtendedData) MarshalBinary() ([]byte, error) {
	buf := NewBuffer(make([]byte, 0, 4))

	buf.AppendUint8(d.value ^ 0x2a)

	return buf.Bytes(), nil
}

func (d *testExtendedData) UnmarshalBinary(data []byte) error {
	buf := NewBuffer(data)

	v := buf.ConsumeUint8()
	if buf.Err != nil {
		return buf.Err
	}

	d.value = v ^ 0x2a

	return nil
}

var _ Packet = &ExtendedPacket{}

func TestExtendedPacketNoData(t *testing.T) {
	const (
		id              = 42
		extendedRequest = "foo@example"
	)

	p := &ExtendedPacket{
		ExtendedRequest: extendedRequest,
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 20,
		200,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 11, 'f', 'o', 'o', '@', 'e', 'x', 'a', 'm', 'p', 'l', 'e',
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = ExtendedPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.ExtendedRequest != extendedRequest {
		t.Errorf("UnmarshalPacketBody(): ExtendedRequest was %q, but expected %q", p.ExtendedRequest, extendedRequest)
	}
}

func TestExtendedPacketTestData(t *testing.T) {
	const (
		id              = 42
		extendedRequest = "foo@example"
		textValue       = 13
	)

	const value = 13

	p := &ExtendedPacket{
		ExtendedRequest: extendedRequest,
		Data: &testExtendedData{
			value: textValue,
		},
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 21,
		200,
		0x00, 0x00, 0x00, 42,
		0x00, 0x00, 0x00, 11, 'f', 'o', 'o', '@', 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x27,
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = ExtendedPacket{
		Data: new(testExtendedData),
	}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.ExtendedRequest != extendedRequest {
		t.Errorf("UnmarshalPacketBody(): ExtendedRequest was %q, but expected %q", p.ExtendedRequest, extendedRequest)
	}

	if buf, ok := p.Data.(*testExtendedData); !ok {
		t.Errorf("UnmarshalPacketBody(): Data was type %T, but expected %T", p.Data, buf)

	} else if buf.value != value {
		t.Errorf("UnmarshalPacketBody(): Data.value was %#x, but expected %#x", buf.value, value)
	}

	*p = ExtendedPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.ExtendedRequest != extendedRequest {
		t.Errorf("UnmarshalPacketBody(): ExtendedRequest was %q, but expected %q", p.ExtendedRequest, extendedRequest)
	}

	wantBuffer := []byte{0x27}

	if buf, ok := p.Data.(*Buffer); !ok {
		t.Errorf("UnmarshalPacketBody(): Data was type %T, but expected %T", p.Data, buf)

	} else if !bytes.Equal(buf.b, wantBuffer) {
		t.Errorf("UnmarshalPacketBody(): Data was %X, but expected %X", buf.b, wantBuffer)
	}
}

var _ Packet = &ExtendedReplyPacket{}

func TestExtendedReplyNoData(t *testing.T) {
	const (
		id = 42
	)

	p := &ExtendedReplyPacket{}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 5,
		201,
		0x00, 0x00, 0x00, 42,
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = ExtendedReplyPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}
}

func TestExtendedReplyPacketTestData(t *testing.T) {
	const (
		id        = 42
		textValue = 13
	)

	const value = 13

	p := &ExtendedReplyPacket{
		Data: &testExtendedData{
			value: textValue,
		},
	}

	buf, err := ComposePacket(p.MarshalPacket(id, nil))
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 6,
		201,
		0x00, 0x00, 0x00, 42,
		0x27,
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalPacket() = %X, but wanted %X", buf, want)
	}

	*p = ExtendedReplyPacket{
		Data: new(testExtendedData),
	}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if buf, ok := p.Data.(*testExtendedData); !ok {
		t.Errorf("UnmarshalPacketBody(): Data was type %T, but expected %T", p.Data, buf)

	} else if buf.value != value {
		t.Errorf("UnmarshalPacketBody(): Data.value was %#x, but expected %#x", buf.value, value)
	}

	*p = ExtendedReplyPacket{}

	// UnmarshalPacketBody assumes the (length, type, request-id) have already been consumed.
	if err := p.UnmarshalPacketBody(NewBuffer(buf[9:])); err != nil {
		t.Fatal("unexpected error:", err)
	}

	wantBuffer := []byte{0x27}

	if buf, ok := p.Data.(*Buffer); !ok {
		t.Errorf("UnmarshalPacketBody(): Data was type %T, but expected %T", p.Data, buf)

	} else if !bytes.Equal(buf.b, wantBuffer) {
		t.Errorf("UnmarshalPacketBody(): Data was %X, but expected %X", buf.b, wantBuffer)
	}
}
