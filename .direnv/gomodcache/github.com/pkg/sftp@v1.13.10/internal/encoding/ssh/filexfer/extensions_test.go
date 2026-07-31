package sshfx

import (
	"bytes"
	"testing"
)

func TestExtensionPair(t *testing.T) {
	const (
		name = "foo"
		data = "1"
	)

	pair := &ExtensionPair{
		Name: name,
		Data: data,
	}

	buf, err := pair.MarshalBinary()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 3,
		'f', 'o', 'o',
		0x00, 0x00, 0x00, 1,
		'1',
	}

	if !bytes.Equal(buf, want) {
		t.Errorf("ExtensionPair.MarshalBinary() = %X, but wanted %X", buf, want)
	}

	*pair = ExtensionPair{}

	if err := pair.UnmarshalBinary(buf); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if pair.Name != name {
		t.Errorf("ExtensionPair.UnmarshalBinary(): Name was %q, but expected %q", pair.Name, name)
	}

	if pair.Data != data {
		t.Errorf("RawPacket.UnmarshalBinary(): Data was %q, but expected %q", pair.Data, data)
	}

}
