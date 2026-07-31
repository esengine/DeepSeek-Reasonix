package sshfx

import (
	"bytes"
	"testing"
)

func TestAttributes(t *testing.T) {
	const (
		size  uint64   = 0x123456789ABCDEF0
		uid            = 1000
		gid            = 100
		perms FileMode = 0x87654321
		atime          = 0x2A2B2C2D
		mtime          = 0x42434445
	)

	extAttr := ExtendedAttribute{
		Type: "foo",
		Data: "bar",
	}

	attr := &Attributes{
		Size:        size,
		UID:         uid,
		GID:         gid,
		Permissions: perms,
		ATime:       atime,
		MTime:       mtime,
		ExtendedAttributes: []ExtendedAttribute{
			extAttr,
		},
	}

	type test struct {
		name    string
		flags   uint32
		encoded []byte
	}

	tests := []test{
		{
			name: "empty",
			encoded: []byte{
				0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			name:  "size",
			flags: AttrSize,
			encoded: []byte{
				0x00, 0x00, 0x00, 0x01,
				0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0,
			},
		},
		{
			name:  "uidgid",
			flags: AttrUIDGID,
			encoded: []byte{
				0x00, 0x00, 0x00, 0x02,
				0x00, 0x00, 0x03, 0xE8,
				0x00, 0x00, 0x00, 100,
			},
		},
		{
			name:  "permissions",
			flags: AttrPermissions,
			encoded: []byte{
				0x00, 0x00, 0x00, 0x04,
				0x87, 0x65, 0x43, 0x21,
			},
		},
		{
			name:  "acmodtime",
			flags: AttrACModTime,
			encoded: []byte{
				0x00, 0x00, 0x00, 0x08,
				0x2A, 0x2B, 0x2C, 0x2D,
				0x42, 0x43, 0x44, 0x45,
			},
		},
		{
			name:  "extended",
			flags: AttrExtended,
			encoded: []byte{
				0x80, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x03, 'f', 'o', 'o',
				0x00, 0x00, 0x00, 0x03, 'b', 'a', 'r',
			},
		},
		{
			name:  "size uidgid permisssions acmodtime extended",
			flags: AttrSize | AttrUIDGID | AttrPermissions | AttrACModTime | AttrExtended,
			encoded: []byte{
				0x80, 0x00, 0x00, 0x0F,
				0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0,
				0x00, 0x00, 0x03, 0xE8,
				0x00, 0x00, 0x00, 100,
				0x87, 0x65, 0x43, 0x21,
				0x2A, 0x2B, 0x2C, 0x2D,
				0x42, 0x43, 0x44, 0x45,
				0x00, 0x00, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x03, 'f', 'o', 'o',
				0x00, 0x00, 0x00, 0x03, 'b', 'a', 'r',
			},
		},
	}

	for _, tt := range tests {
		attr := *attr

		t.Run(tt.name, func(t *testing.T) {
			attr.Flags = tt.flags

			buf, err := attr.MarshalBinary()
			if err != nil {
				t.Fatal("unexpected error:", err)
			}

			if !bytes.Equal(buf, tt.encoded) {
				t.Fatalf("MarshalBinary() = %X, but wanted %X", buf, tt.encoded)
			}

			attr = Attributes{}

			if err := attr.UnmarshalBinary(buf); err != nil {
				t.Fatal("unexpected error:", err)
			}

			if attr.Flags != tt.flags {
				t.Errorf("UnmarshalBinary(): Flags was %x, but wanted %x", attr.Flags, tt.flags)
			}

			if attr.Flags&AttrSize != 0 && attr.Size != size {
				t.Errorf("UnmarshalBinary(): Size was %x, but wanted %x", attr.Size, size)
			}

			if attr.Flags&AttrUIDGID != 0 {
				if attr.UID != uid {
					t.Errorf("UnmarshalBinary(): UID was %x, but wanted %x", attr.UID, uid)
				}

				if attr.GID != gid {
					t.Errorf("UnmarshalBinary(): GID was %x, but wanted %x", attr.GID, gid)
				}
			}

			if attr.Flags&AttrPermissions != 0 && attr.Permissions != perms {
				t.Errorf("UnmarshalBinary(): Permissions was %#v, but wanted %#v", attr.Permissions, perms)
			}

			if attr.Flags&AttrACModTime != 0 {
				if attr.ATime != atime {
					t.Errorf("UnmarshalBinary(): ATime was %x, but wanted %x", attr.ATime, atime)
				}

				if attr.MTime != mtime {
					t.Errorf("UnmarshalBinary(): MTime was %x, but wanted %x", attr.MTime, mtime)
				}
			}

			if attr.Flags&AttrExtended != 0 {
				extAttrs := attr.ExtendedAttributes

				if count := len(extAttrs); count != 1 {
					t.Fatalf("UnmarshalBinary(): len(ExtendedAttributes) was %d, but wanted %d", count, 1)
				}

				if got := extAttrs[0]; got != extAttr {
					t.Errorf("UnmarshalBinary(): ExtendedAttributes[0] was %#v, but wanted %#v", got, extAttr)
				}
			}
		})
	}
}

func TestNameEntry(t *testing.T) {
	const (
		filename          = "foo"
		longname          = "bar"
		perms    FileMode = 0x87654321
	)

	e := &NameEntry{
		Filename: filename,
		Longname: longname,
		Attrs: Attributes{
			Flags:       AttrPermissions,
			Permissions: perms,
		},
	}

	buf, err := e.MarshalBinary()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := []byte{
		0x00, 0x00, 0x00, 0x03, 'f', 'o', 'o',
		0x00, 0x00, 0x00, 0x03, 'b', 'a', 'r',
		0x00, 0x00, 0x00, 0x04,
		0x87, 0x65, 0x43, 0x21,
	}

	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalBinary() = %X, but wanted %X", buf, want)
	}

	*e = NameEntry{}

	if err := e.UnmarshalBinary(buf); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if e.Filename != filename {
		t.Errorf("UnmarhsalFrom(): Filename was %q, but expected %q", e.Filename, filename)
	}

	if e.Longname != longname {
		t.Errorf("UnmarhsalFrom(): Longname was %q, but expected %q", e.Longname, longname)
	}

	if e.Attrs.Flags != AttrPermissions {
		t.Errorf("UnmarshalBinary(): Attrs.Flag was %#x, but expected %#x", e.Attrs.Flags, AttrPermissions)
	}

	if e.Attrs.Permissions != perms {
		t.Errorf("UnmarshalBinary(): Attrs.Permissions was %#v, but expected %#v", e.Attrs.Permissions, perms)
	}
}
