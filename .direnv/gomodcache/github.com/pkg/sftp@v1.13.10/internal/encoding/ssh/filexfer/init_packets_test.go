package sshfx

import (
	"bytes"
	"testing"
)

func TestInitPacket(t *testing.T) {
	var version uint8 = 3

	p := &InitPacket{
		Version: uint32(version),
		Extensions: []*ExtensionPair{
			{
				Name: "foo",
				Data: "1",
			},
		},
	}

	buf, err := p.MarshalBinary()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 17,
		1,
		0x00, 0x00, 0x00, version,
		0x00, 0x00, 0x00, 3, 'f', 'o', 'o',
		0x00, 0x00, 0x00, 1, '1',
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalBinary() = %X, but wanted %X", buf, want)
	}

	*p = InitPacket{}

	// UnmarshalBinary assumes the uint32(length) + uint8(type) have already been consumed.
	if err := p.UnmarshalBinary(buf[5:]); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Version != uint32(version) {
		t.Errorf("UnmarshalBinary(): Version was %d, but expected %d", p.Version, version)
	}

	if len(p.Extensions) != 1 {
		t.Fatalf("UnmarshalBinary(): len(p.Extensions) was %d, but expected %d", len(p.Extensions), 1)
	}

	if got, want := p.Extensions[0].Name, "foo"; got != want {
		t.Errorf("UnmarshalBinary(): p.Extensions[0].Name was %q, but expected %q", got, want)
	}

	if got, want := p.Extensions[0].Data, "1"; got != want {
		t.Errorf("UnmarshalBinary(): p.Extensions[0].Data was %q, but expected %q", got, want)
	}
}

func TestVersionPacket(t *testing.T) {
	var version uint8 = 3

	p := &VersionPacket{
		Version: uint32(version),
		Extensions: []*ExtensionPair{
			{
				Name: "foo",
				Data: "1",
			},
		},
	}

	buf, err := p.MarshalBinary()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 17,
		2,
		0x00, 0x00, 0x00, version,
		0x00, 0x00, 0x00, 3, 'f', 'o', 'o',
		0x00, 0x00, 0x00, 1, '1',
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalBinary() = %X, but wanted %X", buf, want)
	}

	*p = VersionPacket{}

	// UnmarshalBinary assumes the uint32(length) + uint8(type) have already been consumed.
	if err := p.UnmarshalBinary(buf[5:]); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if p.Version != uint32(version) {
		t.Errorf("UnmarshalBinary(): Version was %d, but expected %d", p.Version, version)
	}

	if len(p.Extensions) != 1 {
		t.Fatalf("UnmarshalBinary(): len(p.Extensions) was %d, but expected %d", len(p.Extensions), 1)
	}

	if got, want := p.Extensions[0].Name, "foo"; got != want {
		t.Errorf("UnmarshalBinary(): p.Extensions[0].Name was %q, but expected %q", got, want)
	}

	if got, want := p.Extensions[0].Data, "1"; got != want {
		t.Errorf("UnmarshalBinary(): p.Extensions[0].Data was %q, but expected %q", got, want)
	}
}
