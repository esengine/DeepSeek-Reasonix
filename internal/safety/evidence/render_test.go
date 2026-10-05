package evidence

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func writeReceipt(path string) Receipt {
	args, _ := json.Marshal(map[string]string{"path": path})
	return Receipt{ToolName: "write_file", Args: args, Success: true, Write: true, Paths: []string{path}}
}

func screenshotOf(abs string) Receipt {
	return Receipt{ToolName: "browser_read", Success: true, Read: true, Viewed: []string{abs}}
}

func TestRendersInBrowserReadsTheExtensionOnly(t *testing.T) {
	for path, want := range map[string]bool{
		"pelican.svg": true, "site/Index.HTML": true, "a.htm": true,
		"main.go": false, "App.tsx": false, "style.css": false, "": false,
	} {
		if got := RendersInBrowser(path); got != want {
			t.Errorf("RendersInBrowser(%q) = %v, want %v", path, got, want)
		}
	}
}

// Writing a page owes a look; a screenshot of that file after the write settles
// it; writing it again owes a new one, because the look was of the old bytes.
func TestUnseenRendersFollowWritesAndLooks(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "Pelican.svg")
	var l Ledger
	l.Record(writeReceipt("Pelican.svg"))
	l.Record(writeReceipt("main.go"))

	owed := l.UnseenRenders(root)
	if len(owed) != 1 || owed[0].Kind != ObligationUnseenRender {
		t.Fatalf("owed = %+v, want one unseen render for the svg only", owed)
	}
	if !strings.Contains(owed[0].Discharge, FileURL(abs)) {
		t.Fatalf("discharge %q does not name %s", owed[0].Discharge, FileURL(abs))
	}
	first := owed[0].ID

	l.Record(screenshotOf(ViewedPath(FileURL(abs))))
	if owed := l.UnseenRenders(root); len(owed) != 0 {
		t.Fatalf("a screenshot of the file left %+v owed", owed)
	}

	l.Record(writeReceipt("Pelican.svg"))
	owed = l.UnseenRenders(root)
	if len(owed) != 1 || owed[0].ID == first {
		t.Fatalf("after a rewrite owed = %+v, want a new debt distinct from %s", owed, first)
	}
}

// A look at a different file, a failed write, and a look taken before the
// write all leave the debt standing.
func TestUnseenRendersIgnoresLooksThatAreNotAtTheWrittenFile(t *testing.T) {
	root := t.TempDir()
	var l Ledger
	l.Record(screenshotOf(ViewedPath(FileURL(filepath.Join(root, "page.html")))))
	l.Record(writeReceipt("page.html"))
	l.Record(screenshotOf(ViewedPath(FileURL(filepath.Join(root, "other.html")))))
	failed := writeReceipt("broken.svg")
	failed.Success = false
	l.Record(failed)
	owed := l.UnseenRenders(root)
	if len(owed) != 1 || !strings.Contains(owed[0].Cause, "page.html") {
		t.Fatalf("owed = %+v, want only page.html", owed)
	}
}

// A shell that creates the file is a writer too, or `cat > x.svg` would owe
// nothing a write_file owes.
func TestUnseenRendersCountsFilesACommandCreated(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "out.svg")
	var l Ledger
	l.Record(Receipt{ToolName: "bash", Command: "python draw.py", Success: true, Mutation: true, Created: []string{abs}})
	if owed := l.UnseenRenders(root); len(owed) != 1 {
		t.Fatalf("owed = %+v, want the created svg", owed)
	}
}

func TestViewedPathReadsOnlyLocalFiles(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "a.svg")
	if got, want := ViewedPath(FileURL(abs)), NormalizePath(abs); got != want {
		t.Fatalf("ViewedPath(file url) = %q, want %q", got, want)
	}
	for _, u := range []string{"https://example.com/a.svg", "about:blank", "", "::bad"} {
		if got := ViewedPath(u); got != "" {
			t.Errorf("ViewedPath(%q) = %q, want empty", u, got)
		}
	}
}

// A task that only drew pages is checked by looking at them; one that also
// changed anything else, or changed something it could not name, is not.
func TestOnlyRendersSeenCoversDrawingAlone(t *testing.T) {
	root := t.TempDir()
	look := screenshotOf(ViewedPath(FileURL(filepath.Join(root, "pelican.svg"))))
	cases := map[string]struct {
		receipts []Receipt
		want     bool
	}{
		"drawn and seen":     {[]Receipt{writeReceipt("pelican.svg"), look}, true},
		"drawn, not seen":    {[]Receipt{writeReceipt("pelican.svg")}, false},
		"code changed too":   {[]Receipt{writeReceipt("main.go"), writeReceipt("pelican.svg"), look}, false},
		"unnamed change too": {[]Receipt{{ToolName: "bash", Command: "sed -i s/a/b/ x", Success: true, Mutation: true}, writeReceipt("pelican.svg"), look}, false},
		"nothing written":    {nil, false},
	}
	for name, c := range cases {
		var l Ledger
		for _, r := range c.receipts {
			l.Record(r)
		}
		if got := l.OnlyRendersSeen(root); got != c.want {
			t.Errorf("%s: OnlyRendersSeen = %v, want %v", name, got, c.want)
		}
	}
}
