package gitstatus

import "testing"

// git spells a binary file's counts "-". Reading that as zero says the file
// changed by nothing, which is a different answer from not knowing.
func TestBinaryCountsStayUnsaidRatherThanZero(t *testing.T) {
	got := ParseNumstatZ([]byte("12\t3\tsrc/main.go\x00-\t-\tlogo.png\x00"))
	text, ok := got["src/main.go"]
	if !ok || text.added == nil || *text.added != 12 || text.removed == nil || *text.removed != 3 {
		t.Fatalf("text file = %+v", text)
	}
	binary, ok := got["logo.png"]
	if !ok {
		t.Fatal("the binary file was dropped from the listing")
	}
	if binary.added != nil || binary.removed != nil {
		t.Fatalf("binary counts = %v/%v, want both unsaid", binary.added, binary.removed)
	}
}
