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

const embedGoSource = `package p

type Base struct {
	Mode string ` + "`json:\"mode\"`" + `
}

type Outer struct {
	Base
	Path   string ` + "`json:\"path\"`" + `
	Extra  string ` + "`json:\"extra,omitempty\"`" + `
}
`

const extendsTS = `export interface Base {
  mode: string;
}

export interface Outer extends Base {
  path: string;
  extra?: string;
}
`

var embedPair = []wireMirror{{"t.go", "Outer", tsWireFile, "Outer"}}

func embedFindingsFor(t *testing.T, goSrc, ts string) []Finding {
	t.Helper()
	names, ok := wireFieldNames(parseBytes("t.go", []byte(goSrc)).file, "Outer")
	if !ok {
		t.Fatal("the fixture's Outer does not resolve")
	}
	return wireParityFindings(embedPair, map[string][]string{"t.go.Outer": names}, map[string]string{tsWireFile: ts})
}

// An embedded struct's fields are the outer type's own on the wire, and so are
// an extended interface's.
func TestWireParityFlattensEmbeddingAndExtends(t *testing.T) {
	if got := embedFindingsFor(t, embedGoSource, extendsTS); len(got) != 0 {
		t.Fatalf("a faithful mirror must produce no findings, got %+v", got)
	}
}

func TestWireParityCatchesAFieldAddedToTheEmbeddedStruct(t *testing.T) {
	goSrc := strings.Replace(embedGoSource, "type Outer", "type Wider struct {\n\tMore string `json:\"more\"`\n}\n\ntype Outer", 1)
	goSrc = strings.Replace(goSrc, "\tBase\n", "\tBase\n\tWider\n", 1)
	got := embedFindingsFor(t, goSrc, extendsTS)
	if len(got) != 1 || !strings.Contains(got[0].Msg, `sends "more"`) {
		t.Fatalf("a field added under the embedded struct was not reported: %+v", got)
	}
}

func TestWireParityCatchesAFieldAddedToTheExtendedInterface(t *testing.T) {
	ts := strings.Replace(extendsTS, "  mode: string;\n}\n\nexport interface Outer", "  mode: string;\n  invented: string;\n}\n\nexport interface Outer", 1)
	got := embedFindingsFor(t, embedGoSource, ts)
	if len(got) != 1 || !strings.Contains(got[0].Msg, `reads "invented"`) {
		t.Fatalf("a field added under the extended interface was not reported: %+v", got)
	}
}

func TestWireParityKeepsAnEmbeddedStructWithATagAsOneField(t *testing.T) {
	src := "package p\n\ntype Base struct {\n\tMode string `json:\"mode\"`\n}\n\ntype Outer struct {\n\tBase `json:\"base\"`\n}\n"
	names, ok := wireFieldNames(parseBytes("t.go", []byte(src)).file, "Outer")
	if !ok || len(names) != 1 || names[0] != "base" {
		t.Fatalf("names = %v ok=%v, want the tag's name as one field", names, ok)
	}
}

func TestWireParitySkipsAnEmbeddedTypeFromAnotherPackage(t *testing.T) {
	src := "package p\n\ntype Outer struct {\n\tID string `json:\"id\"`\n\tother.Snapshot\n}\n"
	names, ok := wireFieldNames(parseBytes("t.go", []byte(src)).file, "Outer")
	if !ok || len(names) != 1 || names[0] != "id" {
		t.Fatalf("names = %v ok=%v, want only the struct's own field", names, ok)
	}
}

const partialWriter = `package p

type Item struct {
	ID      string ` + "`json:\"id\"`" + `
	Origin  string ` + "`json:\"origin,omitempty\"`" + `
	Preview string ` + "`json:\"preview\"`" + `
}
`

const partialReader = `package p

type ItemView struct {
	ID      string ` + "`json:\"id\"`" + `
	Origin  string ` + "`json:\"origin\"`" + `
	Preview string ` + "`json:\"preview\"`" + `
}
`

var partialPair = []partialWireMirror{
	{"w.go", "Item", "r.go", "ItemView", []string{"id", "origin", "preview"}},
}

func partialFindingsFor(t *testing.T, writer, reader string) []Finding {
	t.Helper()
	w, r := parseBytes("w.go", []byte(writer)), parseBytes("r.go", []byte(reader))
	wn, ok := wireFieldNames(w.file, "Item")
	if !ok {
		t.Fatal("the fixture's writer does not resolve")
	}
	rn, ok := wireFieldNames(r.file, "ItemView")
	if !ok {
		t.Fatal("the fixture's reader does not resolve")
	}
	return partialWireFindings(partialPair, map[string][]string{
		"w.go.Item":     wn,
		"r.go.ItemView": rn,
	}, nil)
}

// A subset is allowed to ignore what the writer sends; the names it declares
// are the whole contract.
func TestPartialWireParityAcceptsACopyThatCarriesItsDeclaredFields(t *testing.T) {
	if got := partialFindingsFor(t, partialWriter, partialReader); len(got) != 0 {
		t.Fatalf("a faithful subset must produce no findings, got %+v", got)
	}
}

// The defect the sessioninbox guard exists for: the copy drops a field a
// decision reads, and the empty value looks like a real answer.
func TestPartialWireParityCatchesAFieldTheCopyDropped(t *testing.T) {
	reader := strings.Replace(partialReader, "\tOrigin  string `json:\"origin\"`\n", "", 1)
	got := partialFindingsFor(t, partialWriter, reader)
	if len(got) != 1 || !strings.Contains(got[0].Msg, `no longer declares "origin"`) {
		t.Fatalf("a dropped decision field was not reported: %+v", got)
	}
}

// A rename on the reading side reads as zero as surely as a dropped field.
func TestPartialWireParityCatchesANameTheCopyInvented(t *testing.T) {
	reader := strings.Replace(partialReader, `json:"origin"`, `json:"orgin"`, 1)
	got := partialFindingsFor(t, partialWriter, reader)
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want the dropped name and the invented one", got)
	}
	var sawDropped, sawInvented bool
	for _, f := range got {
		sawDropped = sawDropped || strings.Contains(f.Msg, `no longer declares "origin"`)
		sawInvented = sawInvented || strings.Contains(f.Msg, `reads "orgin"`)
	}
	if !sawDropped || !sawInvented {
		t.Fatalf("findings = %+v, want both directions named", got)
	}
}

// The writer's side is checked too: a field the kernel stops sending is a
// decision the copy can no longer make.
func TestPartialWireParityCatchesAFieldTheWriterStoppedSending(t *testing.T) {
	writer := strings.Replace(partialWriter, "\tOrigin  string `json:\"origin,omitempty\"`\n", "", 1)
	got := partialFindingsFor(t, writer, partialReader)
	if len(got) != 1 || !strings.Contains(got[0].Msg, `no longer sends "origin"`) {
		t.Fatalf("a field the writer stopped sending was not reported: %+v", got)
	}
}

// The page's copy is TypeScript and read from its interface, not its wording.
func TestPartialWireParityReadsThePagesInterface(t *testing.T) {
	const ts = "export interface ItemView {\n  id: string;\n  origin?: \"host\";\n  preview: string;\n}\n"
	mirrors := []partialWireMirror{{"w.go", "Item", tsWireFile, "ItemView", []string{"id", "origin", "preview"}}}
	names, ok := wireFieldNames(parseBytes("w.go", []byte(partialWriter)).file, "Item")
	if !ok {
		t.Fatal("the fixture's writer does not resolve")
	}
	if got := partialWireFindings(mirrors, map[string][]string{"w.go.Item": names},
		map[string]string{tsWireFile: ts}); len(got) != 0 {
		t.Fatalf("a faithful TS subset must produce no findings, got %+v", got)
	}
	dropped := strings.Replace(ts, "  origin?: \"host\";\n", "", 1)
	got := partialWireFindings(mirrors, map[string][]string{"w.go.Item": names},
		map[string]string{tsWireFile: dropped})
	if len(got) != 1 || !strings.Contains(got[0].Msg, `no longer declares "origin"`) {
		t.Fatalf("a dropped field in the page's copy was not reported: %+v", got)
	}
}
