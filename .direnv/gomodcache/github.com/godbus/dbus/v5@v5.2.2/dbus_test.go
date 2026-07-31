package dbus

import (
	"bytes"
	"reflect"
	"testing"
)

type TestStruct struct {
	TestInt int
	TestStr string
}

func Test_VariantOfStruct(t *testing.T) {
	tester := TestStruct{TestInt: 123, TestStr: "foobar"}
	testerDecoded := []any{123, "foobar"}
	variant := MakeVariant(testerDecoded)
	input := []any{variant}
	var output TestStruct
	if err := Store(input, &output); err != nil {
		t.Fatal(err)
	}
	if tester != output {
		t.Fatalf("%v != %v\n", tester, output)
	}
}

func Test_VariantOfSlicePtr(t *testing.T) {
	value := []TestStruct{{1, "1"}}
	dest := []*TestStruct{}

	param := &Message{
		Type:  TypeMethodCall,
		Flags: FlagNoAutoStart,
		Headers: map[HeaderField]Variant{
			FieldPath:        MakeVariant(ObjectPath("/example")),
			FieldDestination: MakeVariant(""),
			FieldMember:      MakeVariant("call"),
		},
		Body: []any{value},
	}
	param.Headers[FieldSignature] = MakeVariant(SignatureOf(param.Body...))
	buf := new(bytes.Buffer)
	err := param.EncodeTo(buf, nativeEndian)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := DecodeMessage(buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := Store(msg.Body, &dest); err != nil || reflect.DeepEqual(value, dest) {
		t.Log(err)
		t.FailNow()
	}
}
