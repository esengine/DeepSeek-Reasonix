package main

import (
	"strings"
	"testing"
)

// The kernel side of the pair, carrying the three things that decide what is on
// the wire and what only looks like it: a renaming tag, a skipped field, and an
// unexported one.
const wireGoSource = `package p

type Node struct {
	ID       string ` + "`json:\"id\"`" + `
	ParentID string ` + "`json:\"parentId,omitempty\"`" + `
	Wait     string ` + "`json:\"wait,omitempty\"`" + `
	Internal string ` + "`json:\"-\"`" + `
	hidden   string
}
`

const wireTS = `export interface GraphNode {
  id: string;
  parentId?: string;
  // A comment here names nothing; only the declaration does.
  wait?: GraphWait;
}
`

var wirePair = []wireMirror{{"t.go", "Node", tsWireFile, "GraphNode"}}

func wireFindingsFor(t *testing.T, goSrc, ts string) []Finding {
	t.Helper()
	src := parseBytes("t.go", []byte(goSrc))
	names, ok := wireFieldNames(src.file, "Node")
	if !ok {
		t.Fatal("the fixture declares no Node struct")
	}
	return wireParityFindings(wirePair, map[string][]string{"t.go.Node": names}, map[string]string{tsWireFile: ts})
}

// A field the encoder skips is not on the wire, so demanding the mirror carry it
// would fail every contract that has an internal field.
func TestWireParityAcceptsAMirrorOfWhatIsActuallySent(t *testing.T) {
	if got := wireFindingsFor(t, wireGoSource, wireTS); len(got) != 0 {
		t.Fatalf("a faithful mirror must produce no findings, got %+v", got)
	}
}

// The drift this rule exists for: the kernel gains a field, the hand-written
// mirror does not, and nothing else in the tree notices because the two are
// compiled by different toolchains and meet only over JSON.
func TestWireParityCatchesAFieldTheMirrorNeverLearned(t *testing.T) {
	got := wireFindingsFor(t, wireGoSource, strings.Replace(wireTS, "  wait?: GraphWait;\n", "", 1))
	if len(got) != 1 || !strings.Contains(got[0].Msg, `sends "wait"`) {
		t.Fatalf("a field the mirror cannot read was not reported: %+v", got)
	}
}

// The other direction is a defect too: a mirror that reads what nothing sends
// draws from a field that is always absent.
func TestWireParityCatchesAFieldNothingSends(t *testing.T) {
	got := wireFindingsFor(t, wireGoSource, strings.Replace(wireTS, "  wait?: GraphWait;\n", "  wait?: GraphWait;\n  invented?: string;\n", 1))
	if len(got) != 1 || !strings.Contains(got[0].Msg, `reads "invented"`) {
		t.Fatalf("a field nothing sends was not reported: %+v", got)
	}
}

// A pair is declared, so a declaration that no longer resolves has to say so
// rather than pass by being unreadable.
func TestWireParityReportsAPairItCannotRead(t *testing.T) {
	got := wireParityFindings(wirePair, map[string][]string{}, map[string]string{tsWireFile: wireTS})
	if len(got) != 1 || !strings.Contains(got[0].Msg, "not a struct here") {
		t.Fatalf("a declared pair with no Go side was not reported: %+v", got)
	}
	got = wireFindingsFor(t, wireGoSource, "export interface Other {\n  id: string;\n}\n")
	if len(got) != 1 || !strings.Contains(got[0].Msg, "not declared here") {
		t.Fatalf("a declared pair with no TS side was not reported: %+v", got)
	}
}

var readPair = []readMirror{{"reader.go", "Reader", "writer.go", "Writer"}}

func readFindingsFor(reader, writer []string) []Finding {
	return readParityFindings(readPair, map[string][]string{
		"reader.go.Reader": reader,
		"writer.go.Writer": writer,
	})
}

// The writer is the whole record and the reader charts part of it. Ignoring the
// rest is the point of a partial mirror, not a defect.
func TestReadParityAcceptsAReaderThatChartsPartOfTheRecord(t *testing.T) {
	got := readFindingsFor([]string{"steps", "cost"}, []string{"steps", "cost", "currency", "compactions"})
	if len(got) != 0 {
		t.Fatalf("findings = %v, want none", got)
	}
}

// A rename on the writing side reads as zero forever from here, and nothing
// else in the tree notices: the two are compiled together and never compared.
func TestReadParityFailsANameNothingSends(t *testing.T) {
	got := readFindingsFor([]string{"steps", "readiness_check"}, []string{"steps", "readiness_checks"})
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly one", got)
	}
	if !strings.Contains(got[0].Msg, "readiness_check") || got[0].File != "reader.go" {
		t.Fatalf("finding = %+v, want it to name the reader's own field", got[0])
	}
}

func TestReadParityFailsAPairThatIsNotAStruct(t *testing.T) {
	got := readParityFindings(readPair, map[string][]string{"writer.go.Writer": {"steps"}})
	if len(got) != 1 || got[0].File != "reader.go" {
		t.Fatalf("findings = %v, want one against the missing reader", got)
	}
}
