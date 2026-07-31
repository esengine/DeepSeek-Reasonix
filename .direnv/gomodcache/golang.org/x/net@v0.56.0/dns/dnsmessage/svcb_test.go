// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dnsmessage

import (
	"bytes"
	"math"
	"reflect"
	"testing"
)

func TestSVCBParamsRoundTrip(t *testing.T) {
	testSVCBParam := func(t *testing.T, p *SVCParam) {
		t.Helper()
		rr := &SVCBResource{
			Priority: 1,
			Target:   MustNewName("svc.example.com."),
			Params:   []SVCParam{*p},
		}
		buf, err := rr.pack([]byte{}, nil, 0)
		if err != nil {
			t.Fatalf("pack() = %v", err)
		}
		got, n, err := unpackResourceBody(buf, 0, ResourceHeader{Type: TypeSVCB, Length: uint16(len(buf))})
		if err != nil {
			t.Fatalf("unpackResourceBody() = %v", err)
		}
		if n != len(buf) {
			t.Fatalf("unpacked different amount than packed: got = %d, want = %d", n, len(buf))
		}
		if !reflect.DeepEqual(got, rr) {
			t.Fatalf("roundtrip mismatch: got = %#v, want = %#v", got, rr)
		}
	}

	testSVCBParam(t, &SVCParam{Key: SVCParamMandatory, Value: []byte{0x00, 0x01, 0x00, 0x03, 0x00, 0x05}})
	testSVCBParam(t, &SVCParam{Key: SVCParamALPN, Value: []byte{0x02, 'h', '2', 0x02, 'h', '3'}})
	testSVCBParam(t, &SVCParam{Key: SVCParamNoDefaultALPN, Value: []byte{}})
	testSVCBParam(t, &SVCParam{Key: SVCParamPort, Value: []byte{0x1f, 0x90}}) // 8080
	testSVCBParam(t, &SVCParam{Key: SVCParamIPv4Hint, Value: []byte{192, 0, 2, 1, 198, 51, 100, 2}})
	testSVCBParam(t, &SVCParam{Key: SVCParamECH, Value: []byte{0x01, 0x02, 0x03, 0x04}})
	testSVCBParam(t, &SVCParam{Key: SVCParamIPv6Hint, Value: []byte{0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}})
	testSVCBParam(t, &SVCParam{Key: SVCParamDOHPath, Value: []byte("/dns-query{?dns}")})
	testSVCBParam(t, &SVCParam{Key: SVCParamOHTTP, Value: []byte{0x00, 0x01, 0x02, 0x03}})
	testSVCBParam(t, &SVCParam{Key: SVCParamTLSSupportedGroups, Value: []byte{0x00, 0x1d, 0x00, 0x17}})
}

func TestSVCBParsingAllocs(t *testing.T) {
	name := MustNewName("foo.bar.example.com.")
	msg := Message{
		Header:    Header{Response: true, Authoritative: true},
		Questions: []Question{{Name: name, Type: TypeA, Class: ClassINET}},
		Answers: []Resource{{
			Header: ResourceHeader{Name: name, Type: TypeSVCB, Class: ClassINET, TTL: 300},
			Body: &SVCBResource{
				Priority: 1,
				Target:   MustNewName("svc.example.com."),
				Params: []SVCParam{
					{Key: SVCParamMandatory, Value: []byte{0x00, 0x01, 0x00, 0x03, 0x00, 0x05}},
					{Key: SVCParamALPN, Value: []byte{0x02, 'h', '2', 0x02, 'h', '3'}},
					{Key: SVCParamPort, Value: []byte{0x1f, 0x90}}, // 8080
					{Key: SVCParamIPv4Hint, Value: []byte{192, 0, 2, 1, 198, 51, 100, 2}},
					{Key: SVCParamIPv6Hint, Value: []byte{0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}},
				},
			},
		}},
	}
	buf, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}

	allocs := int(testing.AllocsPerRun(1, func() {
		var p Parser
		if _, err := p.Start(buf); err != nil {
			t.Fatal("Parser.Start(non-nil) =", err)
		}
		if err := p.SkipAllQuestions(); err != nil {
			t.Fatal("Parser.SkipAllQuestions(non-nil) =", err)
		}
		if _, err = p.AnswerHeader(); err != nil {
			t.Fatal("Parser.AnswerHeader(non-nil) =", err)
		}
		if _, err = p.SVCBResource(); err != nil {
			t.Fatal("Parser.SVCBResource(non-nil) =", err)
		}
	}))

	// Make sure we have only two allocations: one for the SVCBResource.Params slice, and one
	// for the SVCParam Values.
	if allocs != 2 {
		t.Errorf("allocations during parsing: got = %d, want 2", allocs)
	}
}

func TestHTTPSBuildAllocs(t *testing.T) {
	b := NewBuilder([]byte{}, Header{Response: true, Authoritative: true})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		t.Fatalf("StartQuestions() = %v", err)
	}
	if err := b.Question(Question{Name: MustNewName("foo.bar.example.com."), Type: TypeHTTPS, Class: ClassINET}); err != nil {
		t.Fatalf("Question() = %v", err)
	}
	if err := b.StartAnswers(); err != nil {
		t.Fatalf("StartAnswers() = %v", err)
	}

	header := ResourceHeader{Name: MustNewName("foo.bar.example.com."), Type: TypeHTTPS, Class: ClassINET, TTL: 300}
	resource := HTTPSResource{SVCBResource{Priority: 1, Target: MustNewName("svc.example.com.")}}

	// AllocsPerRun runs the function once to "warm up" before running the measurement.
	// So technically this function is running twice, on different data, which can potentially
	// make the measurement inaccurate (e.g. by using the name cache the second time).
	// So we make sure we don't run in the warm-up phase.
	warmUp := true
	allocs := int(testing.AllocsPerRun(1, func() {
		if warmUp {
			warmUp = false
			return
		}
		if err := b.HTTPSResource(header, resource); err != nil {
			t.Fatalf("HTTPSResource() = %v", err)
		}
	}))
	if allocs != 1 {
		t.Fatalf("unexpected allocations: got = %d, want = 1", allocs)
	}
}

func TestSVCBParams(t *testing.T) {
	rr := SVCBResource{Priority: 1, Target: MustNewName("svc.example.com.")}
	if _, ok := rr.GetParam(SVCParamALPN); ok {
		t.Fatal("GetParam found non-existent param")
	}
	rr.SetParam(SVCParamIPv4Hint, []byte{192, 0, 2, 1})
	inALPN := []byte{0x02, 'h', '2', 0x02, 'h', '3'}
	rr.SetParam(SVCParamALPN, inALPN)

	// Check sorting of params
	packed, err := rr.pack([]byte{}, nil, 0)
	if err != nil {
		t.Fatal("pack() =", err)
	}
	expectedBytes := []byte{
		0x00, 0x01, // priority
		0x03, 0x73, 0x76, 0x63, 0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, // target
		0x00, 0x01, // key 1
		0x00, 0x06, // length 6
		0x02, 'h', '2', 0x02, 'h', '3', // value
		0x00, 0x04, // key 4
		0x00, 0x04, // length 4
		192, 0, 2, 1, // value
	}
	if !reflect.DeepEqual(packed, expectedBytes) {
		t.Fatalf("pack() produced unexpected output: want = %v, got = %v", expectedBytes, packed)
	}

	// Check GetParam and DeleteParam.
	if outALPN, ok := rr.GetParam(SVCParamALPN); !ok || !bytes.Equal(outALPN, inALPN) {
		t.Fatal("GetParam failed to retrieve set param")
	}
	if !rr.DeleteParam(SVCParamALPN) {
		t.Fatal("DeleteParam failed to remove existing param")
	}
	if _, ok := rr.GetParam(SVCParamALPN); ok {
		t.Fatal("GetParam found deleted param")
	}
	if len(rr.Params) != 1 || rr.Params[0].Key != SVCParamIPv4Hint {
		t.Fatalf("DeleteParam removed wrong param: got = %#v, want = [%#v]", rr.Params, SVCParam{Key: SVCParamIPv4Hint, Value: []byte{192, 0, 2, 1}})
	}
}

func TestSVCBWireFormat(t *testing.T) {
	testRecord := func(bytesInput []byte, parsedInput *SVCBResource) {
		parsedOutput, n, err := unpackResourceBody(bytesInput, 0, ResourceHeader{Type: TypeSVCB, Length: uint16(len(bytesInput))})
		if err != nil {
			t.Fatalf("unpackResourceBody() = %v", err)
		}
		if n != len(bytesInput) {
			t.Fatalf("unpacked different amount than packed: got = %d, want = %d", n, len(bytesInput))
		}
		if !reflect.DeepEqual(parsedOutput, parsedInput) {
			t.Fatalf("unpack mismatch: got = %#v, want = %#v", parsedOutput, parsedInput)
		}

		bytesOutput, err := parsedInput.pack([]byte{}, nil, 0)
		if err != nil {
			t.Fatalf("pack() = %v", err)
		}
		if !reflect.DeepEqual(bytesOutput, bytesInput) {
			t.Fatalf("pack mismatch: got = %#v, want = %#v", bytesOutput, bytesInput)
		}
	}
	// Test examples from https://datatracker.ietf.org/doc/html/rfc9460#name-test-vectors

	// Example D.1. Alias Mode

	// Figure 2: AliasMode
	// example.com.   HTTPS   0 foo.example.com.
	bytes := []byte{
		0x00, 0x00, // priority
		0x03, 0x66, 0x6f, 0x6f, 0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, // target: foo.example.com.
	}
	parsed := &SVCBResource{
		Priority: 0,
		Target:   MustNewName("foo.example.com."),
		Params:   []SVCParam{},
	}
	testRecord(bytes, parsed)

	// Example D.2. Service Mode

	// Figure 3: TargetName Is "."
	// example.com.   SVCB   1 .
	bytes = []byte{
		0x00, 0x01, // priority
		0x00, // target (root label)
	}
	parsed = &SVCBResource{
		Priority: 1,
		Target:   MustNewName("."),
		Params:   []SVCParam{},
	}
	testRecord(bytes, parsed)

	// Figure 4: Specifies a Port
	// example.com.   SVCB   16 foo.example.com. port=53
	bytes = []byte{
		0x00, 0x10, // priority
		0x03, 0x66, 0x6f, 0x6f, 0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, // target
		0x00, 0x03, // key 3
		0x00, 0x02, // length 2
		0x00, 0x35, // value
	}
	parsed = &SVCBResource{
		Priority: 16,
		Target:   MustNewName("foo.example.com."),
		Params:   []SVCParam{{Key: SVCParamPort, Value: []byte{0x00, 0x35}}},
	}
	testRecord(bytes, parsed)

	// Figure 5: A Generic Key and Unquoted Value
	// example.com.   SVCB   1 foo.example.com. key667=hello
	bytes = []byte{
		0x00, 0x01, // priority
		0x03, 0x66, 0x6f, 0x6f, 0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, // target
		0x02, 0x9b, // key 667
		0x00, 0x05, // length 5
		0x68, 0x65, 0x6c, 0x6c, 0x6f, // value
	}
	parsed = &SVCBResource{
		Priority: 1,
		Target:   MustNewName("foo.example.com."),
		Params:   []SVCParam{{Key: 667, Value: []byte("hello")}},
	}
	testRecord(bytes, parsed)

	// Figure 6: A Generic Key and Quoted Value with a Decimal Escape
	// example.com.   SVCB   1 foo.example.com. key667="hello\210qoo"
	bytes = []byte{
		0x00, 0x01, // priority
		0x03, 0x66, 0x6f, 0x6f, 0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, // target
		0x02, 0x9b, // key 667
		0x00, 0x09, // length 9
		0x68, 0x65, 0x6c, 0x6c, 0x6f, 0xd2, 0x71, 0x6f, 0x6f, // value
	}
	parsed = &SVCBResource{
		Priority: 1,
		Target:   MustNewName("foo.example.com."),
		Params:   []SVCParam{{Key: 667, Value: []byte("hello\xd2qoo")}},
	}
	testRecord(bytes, parsed)

	// Figure 7: Two Quoted IPv6 Hints
	// example.com.   SVCB   1 foo.example.com. (
	//                       ipv6hint="2001:db8::1,2001:db8::53:1"
	//                       )
	bytes = []byte{
		0x00, 0x01, // priority
		0x03, 0x66, 0x6f, 0x6f, 0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, // target
		0x00, 0x06, // key 6
		0x00, 0x20, // length 32
		0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // first address
		0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x53, 0x00, 0x01, // second address
	}
	parsed = &SVCBResource{
		Priority: 1,
		Target:   MustNewName("foo.example.com."),
		Params:   []SVCParam{{Key: SVCParamIPv6Hint, Value: []byte{0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x53, 0x00, 0x01}}},
	}
	testRecord(bytes, parsed)

	// Figure 8: An IPv6 Hint Using the Embedded IPv4 Syntax
	// example.com.   SVCB   1 example.com. (
	//                        ipv6hint="2001:db8:122:344::192.0.2.33"
	//                        )
	bytes = []byte{
		0x00, 0x01, // priority
		0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, // target
		0x00, 0x06, // key 6
		0x00, 0x10, // length 16
		0x20, 0x01, 0x0d, 0xb8, 0x01, 0x22, 0x03, 0x44, 0x00, 0x00, 0x00, 0x00, 0xc0, 0x00, 0x02, 0x21, // address
	}
	parsed = &SVCBResource{
		Priority: 1,
		Target:   MustNewName("example.com."),
		Params:   []SVCParam{{Key: SVCParamIPv6Hint, Value: []byte{0x20, 0x01, 0x0d, 0xb8, 0x01, 0x22, 0x03, 0x44, 0x00, 0x00, 0x00, 0x00, 0xc0, 0x00, 0x02, 0x21}}},
	}
	testRecord(bytes, parsed)

	// Figure 9: SvcParamKey Ordering Is Arbitrary in Presentation Format but Sorted in Wire Format
	// example.com.   SVCB   16 foo.example.org. (
	//                      alpn=h2,h3-19 mandatory=ipv4hint,alpn
	//                      ipv4hint=192.0.2.1
	//                      )
	bytes = []byte{
		0x00, 0x10, // priority
		0x03, 0x66, 0x6f, 0x6f, 0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x6f, 0x72, 0x67, 0x00, // target
		0x00, 0x00, // key 0
		0x00, 0x04, // param length 4
		0x00, 0x01, // value: key 1
		0x00, 0x04, // value: key 4
		0x00, 0x01, // key 1
		0x00, 0x09, // param length 9
		0x02,       // alpn length 2
		0x68, 0x32, // alpn value
		0x05,                         // alpn length 5
		0x68, 0x33, 0x2d, 0x31, 0x39, // alpn value
		0x00, 0x04, // key 4
		0x00, 0x04, // param length 4
		0xc0, 0x00, 0x02, 0x01, // param value
	}
	parsed = &SVCBResource{
		Priority: 16,
		Target:   MustNewName("foo.example.org."),
		Params: []SVCParam{
			{Key: SVCParamMandatory, Value: []byte{0x00, 0x01, 0x00, 0x04}},
			{Key: SVCParamALPN, Value: []byte{0x02, 0x68, 0x32, 0x05, 0x68, 0x33, 0x2d, 0x31, 0x39}},
			{Key: SVCParamIPv4Hint, Value: []byte{0xc0, 0x00, 0x02, 0x01}},
		},
	}
	testRecord(bytes, parsed)

	// Figure 10: An "alpn" Value with an Escaped Comma and an Escaped Backslash in Two Presentation Formats
	// example.com.   SVCB   16 foo.example.org. alpn=f\\\092oo\092,bar,h2
	bytes = []byte{
		0x00, 0x10, // priority
		0x03, 0x66, 0x6f, 0x6f, 0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x6f, 0x72, 0x67, 0x00, // target
		0x00, 0x01, // key 1
		0x00, 0x0c, // param length 12
		0x08,                                           // alpn length 8
		0x66, 0x5c, 0x6f, 0x6f, 0x2c, 0x62, 0x61, 0x72, // alpn value
		0x02,       // alpn length 2
		0x68, 0x32, // alpn value
	}
	parsed = &SVCBResource{
		Priority: 16,
		Target:   MustNewName("foo.example.org."),
		Params: []SVCParam{
			{Key: SVCParamALPN, Value: []byte{0x08, 0x66, 0x5c, 0x6f, 0x6f, 0x2c, 0x62, 0x61, 0x72, 0x02, 0x68, 0x32}},
		},
	}
	testRecord(bytes, parsed)
}

func TestSVCBPackErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		r    SVCBResource
	}{
		{
			name: "long value",
			r: SVCBResource{
				Target: MustNewName("example.com."),
				Params: []SVCParam{
					{
						Key:   SVCParamMandatory,
						Value: make([]byte, math.MaxUint16+1),
					},
				},
			},
		},
		{
			name: "out-of-order keys",
			r: SVCBResource{
				Target: MustNewName("example.com."),
				Params: []SVCParam{
					{
						Key:   SVCParamPort, // 3
						Value: []byte("443"),
					},
					{
						Key:   SVCParamALPN, // 1
						Value: []byte("h2"),
					},
				},
			},
		},
		{
			name: "duplicate keys",
			r: SVCBResource{
				Target: MustNewName("example.com."),
				Params: []SVCParam{
					{
						Key:   SVCParamALPN,
						Value: []byte("h3"),
					},
					{
						Key:   SVCParamALPN,
						Value: []byte("h2"),
					},
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := NewBuilder(nil, Header{})
			b.StartQuestions()
			b.StartAnswers()
			if err := b.SVCBResource(ResourceHeader{
				Name: MustNewName("example.com."),
			}, test.r); err == nil {
				t.Errorf("b.SVCBResource() succeeded; want error")
			}

			b = NewBuilder(nil, Header{})
			b.StartQuestions()
			b.StartAnswers()
			if err := b.HTTPSResource(ResourceHeader{
				Name: MustNewName("example.com."),
			}, HTTPSResource{test.r}); err == nil {
				t.Errorf("b.HTTPSResource() succeeded; want error")
			}
		})
	}
}
