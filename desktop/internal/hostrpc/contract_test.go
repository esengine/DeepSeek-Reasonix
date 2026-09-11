package hostrpc

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type contractSmall struct{}

func (*contractSmall) One() string { return "" }

type contractLarge struct{}

func (*contractLarge) One() string { return "" }
func (*contractLarge) Two() int    { return 0 }

func TestContractDigestIsStableAndSensitiveToCommands(t *testing.T) {
	small := Build(mustRegistry(t, &contractSmall{}, nil), []string{"b:event", "a:event", "b:event"})
	again := Build(mustRegistry(t, &contractSmall{}, nil), []string{"a:event", "b:event"})
	large := Build(mustRegistry(t, &contractLarge{}, nil), []string{"a:event", "b:event"})
	fewer := Build(mustRegistry(t, &contractSmall{}, nil), []string{"a:event"})

	if small.Digest() != again.Digest() {
		t.Fatalf("digest changed across identical builds: %s vs %s", small.Digest(), again.Digest())
	}
	if small.Digest() == large.Digest() {
		t.Fatal("adding a command must change the digest")
	}
	if small.Digest() == fewer.Digest() {
		t.Fatal("removing an event must change the digest")
	}
	if !strings.HasPrefix(small.Digest(), "sha256:") || len(small.Digest()) != len("sha256:")+64 {
		t.Fatalf("digest format = %q", small.Digest())
	}
	if small.ProtocolVersion != 1 {
		t.Fatalf("protocol version = %d", small.ProtocolVersion)
	}
	if strings.Join(small.Events, ",") != "a:event,b:event" {
		t.Fatalf("events = %v", small.Events)
	}
}

func TestContractCanonicalJSONHasSortedKeysAndNoWhitespace(t *testing.T) {
	c := Build(mustRegistry(t, &fixtureTarget{}, nil), []string{"z", "a"})
	canonical := c.Canonical()
	if bytes.ContainsAny(canonical, "\n\t") || bytes.Contains(canonical, []byte(`": `)) {
		t.Fatal("canonical JSON must not contain layout whitespace")
	}
	if !json.Valid(canonical) {
		t.Fatal("canonical JSON is invalid")
	}
	if !bytes.HasPrefix(canonical, []byte(`{"commands":[{"cancellation":"before-dispatch","name":"Explode","params":[]`)) {
		t.Fatalf("keys not sorted: %.80s", canonical)
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, c); err != nil {
		t.Fatal(err)
	}
	var indented, compact any
	if err := json.Unmarshal(buf.Bytes(), &indented); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(canonical, &compact); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("}\n")) {
		t.Fatal("WriteJSON must end with a newline")
	}
	compactAgain, _ := json.Marshal(indented)
	compactRef, _ := json.Marshal(compact)
	if !bytes.Equal(compactAgain, compactRef) {
		t.Fatal("WriteJSON content differs from Canonical")
	}
}

func TestContractOmitsNilCollectionsAsEmpty(t *testing.T) {
	type bare struct{}
	c := Build(mustRegistry(t, &bare{}, nil), nil)
	canonical := string(c.Canonical())
	for _, want := range []string{`"commands":[]`, `"events":[]`, `"types":{}`} {
		if !strings.Contains(canonical, want) {
			t.Errorf("canonical %s lacks %s", canonical, want)
		}
	}
}
