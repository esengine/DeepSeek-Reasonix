package dbus

import "testing"

func TestMessage_validateHeader(t *testing.T) {
	tcs := []struct {
		msg Message
		err error
	}{
		{
			msg: Message{
				Flags: 0xFF,
			},
			err: InvalidMessageError("invalid flags"),
		},
		{
			msg: Message{
				Type: 0xFF,
			},
			err: InvalidMessageError("invalid message type"),
		},
		{
			msg: Message{
				Type: TypeMethodCall,
				Headers: map[HeaderField]Variant{
					0xFF: MakeVariant("foo"),
				},
			},
			err: InvalidMessageError("invalid header"),
		},
		{
			msg: Message{
				Type: TypeMethodCall,
				Headers: map[HeaderField]Variant{
					FieldPath: MakeVariant(42),
				},
			},
			err: InvalidMessageError("invalid type of header field"),
		},
		{
			msg: Message{
				Type: TypeMethodCall,
			},
			err: InvalidMessageError("missing required header"),
		},
		{
			msg: Message{
				Type: TypeMethodCall,
				Headers: map[HeaderField]Variant{
					FieldPath:   MakeVariant(ObjectPath("break")),
					FieldMember: MakeVariant("foo.bar"),
				},
			},
			err: InvalidMessageError("invalid path name"),
		},
		{
			msg: Message{
				Type: TypeMethodCall,
				Headers: map[HeaderField]Variant{
					FieldPath:   MakeVariant(ObjectPath("/")),
					FieldMember: MakeVariant("2"),
				},
			},
			err: InvalidMessageError("invalid member name"),
		},
		{
			msg: Message{
				Type: TypeMethodCall,
				Headers: map[HeaderField]Variant{
					FieldPath:      MakeVariant(ObjectPath("/")),
					FieldMember:    MakeVariant("foo.bar"),
					FieldInterface: MakeVariant("break"),
				},
			},
			err: InvalidMessageError("invalid interface name"),
		},
		{
			msg: Message{
				Type: TypeError,
				Headers: map[HeaderField]Variant{
					FieldReplySerial: MakeVariant(uint32(0)),
					FieldErrorName:   MakeVariant("break"),
				},
			},
			err: InvalidMessageError("invalid error name"),
		},
		{
			msg: Message{
				Type: TypeError,
				Headers: map[HeaderField]Variant{
					FieldReplySerial: MakeVariant(uint32(0)),
					FieldErrorName:   MakeVariant("error.name"),
				},
				Body: []any{
					"break",
				},
			},
			err: InvalidMessageError("missing signature"),
		},
	}

	for _, tc := range tcs {
		t.Run(tc.err.Error(), func(t *testing.T) {
			err := tc.msg.validateHeader()
			if err != tc.err {
				t.Errorf("expected error %q, got %q", tc.err, err)
			}
		})
	}
}
