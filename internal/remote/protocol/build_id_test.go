package protocol

import (
	"errors"
	"strings"
	"testing"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"

func validBuildID(t *testing.T) BuildID {
	t.Helper()
	id, err := NewBuildID("dev", testRevision)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBuildIDAcceptsCommitAndDirtyCommitButRejectsGenericDevRevision(t *testing.T) {
	clean := validBuildID(t)
	dirty := clean
	dirty.SourceRevision += "+dirty"
	if err := dirty.Validate(); err != nil {
		t.Fatalf("dirty revision rejected: %v", err)
	}
	for _, revision := range []string{"", "dev", "unknown", strings.Repeat("a", 39), strings.Repeat("A", 40)} {
		invalid := clean
		invalid.SourceRevision = revision
		if err := invalid.Validate(); err == nil {
			t.Errorf("revision %q accepted", revision)
		}
	}
}

func TestCurrentBuildIDUsesLinkedSourceRevision(t *testing.T) {
	previous := linkedSourceRevision
	linkedSourceRevision = testRevision
	t.Cleanup(func() { linkedSourceRevision = previous })

	id := CurrentBuildID("v1.2.3")
	if id.ProductVersion != "v1.2.3" || id.SourceRevision != testRevision {
		t.Fatalf("CurrentBuildID() = %+v, want linked revision %q", id, testRevision)
	}
}

func TestCurrentBuildIDPreservesLinkedDirtyRevision(t *testing.T) {
	previous := linkedSourceRevision
	linkedSourceRevision = testRevision + "+dirty"
	t.Cleanup(func() { linkedSourceRevision = previous })

	if got := CurrentBuildID("dev").SourceRevision; got != testRevision+"+dirty" {
		t.Fatalf("CurrentBuildID().SourceRevision = %q, want linked dirty revision", got)
	}
}

func TestBuildIDRejectsEveryFieldMismatch(t *testing.T) {
	expected := validBuildID(t)
	cases := []struct {
		field  BuildIDField
		mutate func(*BuildID)
	}{
		{BuildProductVersion, func(id *BuildID) { id.ProductVersion = "v2" }},
		{BuildSourceRevision, func(id *BuildID) { id.SourceRevision = strings.Repeat("a", 40) }},
		{BuildProtocolVersion, func(id *BuildID) { id.ProtocolVersion = "2" }},
		{BuildSchemaHash, func(id *BuildID) { id.SchemaHash = "sha256:" + strings.Repeat("a", 64) }},
	}
	for _, test := range cases {
		actual := expected
		test.mutate(&actual)
		err := CompareBuildID(expected, actual)
		var mismatch *BuildIDMismatch
		if !errors.As(err, &mismatch) || mismatch.Field != test.field {
			t.Errorf("mismatch %s returned %v", test.field, err)
		}
	}
	if err := CompareBuildID(expected, expected); err != nil {
		t.Fatalf("exact Build ID rejected: %v", err)
	}
}
