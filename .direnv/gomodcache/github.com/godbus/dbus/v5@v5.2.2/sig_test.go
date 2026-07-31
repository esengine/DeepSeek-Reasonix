package dbus

import (
	"testing"
)

type structWithManyFields struct {
	A01 int32
	A02 int32
	A03 int32
	A04 int32
	A05 int32
	A06 int32
	A07 int32
	A08 int32
	A09 int32
	A10 int32
	A11 int32
	A12 int32
	A13 int32
	A14 int32
	A15 int32
	A16 int32
	A17 int32
	A18 int32
	A19 int32
	A20 int32
	A21 int32
	A22 int32
	A23 int32
	A24 int32
	A25 int32
	A26 int32
	A27 int32
	A28 int32
	A29 int32
	A30 int32
	A31 int32
	A32 int32
	A33 int32
}

var sigTests = []struct {
	vs  []any
	sig Signature
}{
	{
		[]any{new(int32)},
		Signature{"i"},
	},
	{
		[]any{new(string)},
		Signature{"s"},
	},
	{
		[]any{new(Signature)},
		Signature{"g"},
	},
	{
		[]any{new([]int16)},
		Signature{"an"},
	},
	{
		[]any{new(int16), new(uint32)},
		Signature{"nu"},
	},
	{
		[]any{new(map[byte]Variant)},
		Signature{"a{yv}"},
	},
	{
		[]any{new(Variant), new([]map[int32]string)},
		Signature{"vaa{is}"},
	},
	{
		[]any{new(structWithManyFields)},
		Signature{"(iiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiii)"},
	},
}

func TestSig(t *testing.T) {
	for i, v := range sigTests {
		sig := SignatureOf(v.vs...)
		if sig != v.sig {
			t.Errorf("test %d: got %q, expected %q", i+1, sig.str, v.sig.str)
		}
	}
}

var getSigTest = []any{
	[]struct {
		B byte
		I int32
		T uint64
		S string
	}{},
	map[string]Variant{},
}

func BenchmarkGetSignatureSimple(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SignatureOf("", int32(0))
	}
}

func BenchmarkGetSignatureLong(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SignatureOf(getSigTest...)
	}
}
