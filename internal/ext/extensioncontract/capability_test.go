package extensioncontract

import "testing"

func TestCapabilityKeyValidate(t *testing.T) {
	if err := (CapabilityKey{Namespace: "reasonix", Kind: "provider", ID: "deepseek/v4"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (CapabilityKey{Namespace: "", Kind: "provider", ID: "x"}).Validate(); err == nil {
		t.Fatal("empty namespace accepted")
	}
}

func TestCapabilityValidateSchemaHash(t *testing.T) {
	c := Capability{
		Key:     CapabilityKey{Namespace: "plugin/example", Kind: "provider", ID: "fake/x"},
		Version: "1.0.0",
	}
	if err := c.Validate(); err == nil {
		t.Fatal("provider without schemaHash accepted")
	}
	c.SchemaHash = "sha256:abc"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRequirementSatisfiedBy(t *testing.T) {
	provided := Capability{
		Key:        CapabilityKey{Namespace: "reasonix", Kind: "provider", ID: "deepseek/v4"},
		Version:    "1.2.0",
		SchemaHash: "sha256:p",
	}
	req := Requirement{
		Capability: Capability{
			Key: CapabilityKey{Namespace: "reasonix", Kind: "provider", ID: "deepseek/v4"},
		},
		VersionRange: ">=1.0.0,<2.0.0",
	}
	if !req.SatisfiedBy(provided) {
		t.Fatal("range should match")
	}
	req.SchemaHash = "sha256:other"
	if req.SatisfiedBy(provided) {
		t.Fatal("schema pin mismatch should fail")
	}
	req.SchemaHash = ""
	req.VersionRange = ">=2.0.0"
	if req.SatisfiedBy(provided) {
		t.Fatal("out of range should fail")
	}
}

func TestCanonicalHashStable(t *testing.T) {
	c := Capability{
		Key:        CapabilityKey{Namespace: "a", Kind: "tool", ID: "t"},
		Version:    "1.0.0",
		SchemaHash: "sha256:x",
	}
	h1 := c.CanonicalHash()
	h2 := c.CanonicalHash()
	if h1 == "" || h1 != h2 {
		t.Fatalf("hash unstable: %q vs %q", h1, h2)
	}
	c.Version = "1.0.1"
	if c.CanonicalHash() == h1 {
		t.Fatal("version change must change hash")
	}
}
