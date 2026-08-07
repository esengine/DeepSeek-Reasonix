package productdocs

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	productcontent "reasonix/docs"
	"reasonix/internal/provider"
	releasenotes "reasonix/release-notes"
)

func TestEmbeddedCatalogLoadsDeterministically(t *testing.T) {
	first, err := loadCatalog(productcontent.Content)
	if err != nil {
		t.Fatalf("loadCatalog: %v", err)
	}
	second, err := loadCatalog(productcontent.Content)
	if err != nil {
		t.Fatalf("loadCatalog second time: %v", err)
	}
	if len(first.docs) < 40 {
		t.Fatalf("embedded docs = %d, want at least 40", len(first.docs))
	}
	if len(first.sections) < len(first.docs) {
		t.Fatalf("embedded sections = %d, docs = %d", len(first.sections), len(first.docs))
	}
	if first.digest == "" || first.digest != second.digest {
		t.Fatalf("digest mismatch: %q != %q", first.digest, second.digest)
	}
	if _, ok := first.byPath["GUIDE.zh-CN.md"]; !ok {
		t.Fatal("Chinese guide is missing from the embedded catalog")
	}
	if _, ok := first.byPath["GUIDE.md"]; !ok {
		t.Fatal("English guide is missing from the embedded catalog")
	}
}

func TestEmbeddedAndSourceManifestsMatch(t *testing.T) {
	embedded, err := EmbeddedManifest()
	if err != nil {
		t.Fatalf("EmbeddedManifest: %v", err)
	}
	source, err := SourceManifest(productcontent.Content, releasenotes.Content)
	if err != nil {
		t.Fatalf("SourceManifest: %v", err)
	}
	if embedded.Digest != source.Digest || embedded.Documents != source.Documents || embedded.Sections != source.Sections {
		t.Fatalf("embedded manifest %#v does not match source %#v", embedded, source)
	}
	if !strings.HasPrefix(embedded.Digest, "sha256:") || embedded.Version == "" || embedded.Revision == "" {
		t.Fatalf("manifest is missing build identity: %#v", embedded)
	}
	if embedded.ReleaseNotes < 10 {
		t.Fatalf("embedded release notes = %d, want release history", embedded.ReleaseNotes)
	}
}

func TestDocsCommandOverviewAndSearchUseEmbeddedCorpus(t *testing.T) {
	overview, err := CommandOverview("zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"内置 Reasonix 文档", "version=", "revision=", "digest=sha256:", "/docs 1.19.5 更新日志"} {
		if !strings.Contains(overview, want) {
			t.Fatalf("command overview missing %q:\n%s", want, overview)
		}
	}
	qualified, err := CommandOverviewFor("en", "/reasonix:docs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(qualified, "Usage: /reasonix:docs <question>") || strings.Contains(qualified, "Example: /docs ") {
		t.Fatalf("qualified command overview used the wrong invocation:\n%s", qualified)
	}

	results, err := SearchEmbedded(context.Background(), "1.19.5 更新日志")
	if err != nil {
		t.Fatal(err)
	}
	firstStart := strings.Index(results, "\n1. ")
	firstEnd := strings.Index(results, "\n2. ")
	if firstStart < 0 || firstEnd <= firstStart || !strings.Contains(results[firstStart:firstEnd], "path=changelog/v1.19.5.zh-CN.md") || !strings.Contains(results, "digest=sha256:") {
		t.Fatalf("command search did not use the embedded release catalog:\n%s", results)
	}
}

func TestExtensionDeveloperGuidesAreEmbeddedAndSearchable(t *testing.T) {
	c, err := loadCatalog(productcontent.Content)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"EXTENSIONS.md",
		"EXTENSIONS.zh-CN.md",
		"EXTENSION_PROTOCOL.md",
		"EXTENSION_PROTOCOL.zh-CN.md",
		"PLUGIN_PACKAGES.md",
		"PLUGIN_PACKAGES.zh-CN.md",
	} {
		if _, ok := c.byPath[path]; !ok {
			t.Fatalf("extension developer guide %q is missing from the embedded catalog", path)
		}
	}

	tool := &docsTool{catalog: c}
	english, err := tool.search(context.Background(), "sidecar Manifest v1 runtime Go SDK", "en", "all", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(english, "path=docs/EXTENSIONS.md") || !strings.Contains(english, "path=docs/PLUGIN_PACKAGES.md") {
		t.Fatalf("English extension search did not expose the overview and manifest reference:\n%s", english)
	}

	chinese, err := tool.search(context.Background(), "Sidecar 插件 Manifest v1 扩展开发", "zh-CN", "all", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chinese, "path=docs/EXTENSIONS.zh-CN.md") || !strings.Contains(chinese, "path=docs/PLUGIN_PACKAGES.zh-CN.md") {
		t.Fatalf("Chinese extension search did not expose the overview and manifest reference:\n%s", chinese)
	}
}

func TestSourceManifestChangesWithMarkdownBytes(t *testing.T) {
	first, err := SourceManifest(fstest.MapFS{"GUIDE.md": {Data: []byte("# Guide\n\nFirst.\n")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SourceManifest(fstest.MapFS{"GUIDE.md": {Data: []byte("# Guide\n\nSecond.\n")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == second.Digest {
		t.Fatalf("different Markdown bytes produced the same digest: %s", first.Digest)
	}
}

func TestSourceManifestDigestUsesUnambiguousFileFraming(t *testing.T) {
	firstA := []byte("# Guide\n\nalpha")
	firstB := []byte("betaB.md\x00# Reference\n\ngamma")
	secondA := []byte("# Guide\n\nalphaB.md\x00beta")
	secondB := []byte("# Reference\n\ngamma")

	legacyDigest := func(a, b []byte) [sha256.Size]byte {
		h := sha256.New()
		for _, record := range []struct {
			name string
			data []byte
		}{{"A.md", a}, {"B.md", b}} {
			_, _ = h.Write([]byte(record.name))
			_, _ = h.Write([]byte{0})
			_, _ = h.Write(record.data)
		}
		var digest [sha256.Size]byte
		copy(digest[:], h.Sum(nil))
		return digest
	}
	if legacyDigest(firstA, firstB) != legacyDigest(secondA, secondB) {
		t.Fatal("test fixture must collide under the legacy delimiter-only framing")
	}

	first, err := SourceManifest(fstest.MapFS{
		"A.md": {Data: firstA},
		"B.md": {Data: firstB},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SourceManifest(fstest.MapFS{
		"A.md": {Data: secondA},
		"B.md": {Data: secondB},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Documents != second.Documents || first.Sections != second.Sections {
		t.Fatalf("fixture changed corpus shape: first=%#v second=%#v", first, second)
	}
	if first.Digest == second.Digest {
		t.Fatalf("different file boundaries produced the same digest: %s", first.Digest)
	}
}

func TestSearchRejectsOversizedQueriesBeforeRetrieval(t *testing.T) {
	oversized := strings.Repeat("文", maxQueryRunes+1)
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "tool",
			run: func() error {
				args, err := json.Marshal(map[string]any{"operation": "search", "query": oversized})
				if err != nil {
					return err
				}
				_, err = NewTool().Execute(context.Background(), args)
				return err
			},
		},
		{
			name: "slash command host path",
			run: func() error {
				_, err := SearchEmbedded(context.Background(), oversized)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil || !strings.Contains(err.Error(), "maximum 4096 characters") {
				t.Fatalf("oversized query error = %v", err)
			}
		})
	}
}

func TestReleaseNotesAreSearchableInBothLanguages(t *testing.T) {
	c, err := loadCatalogWithReleaseNotes(productcontent.Content, releasenotes.Content)
	if err != nil {
		t.Fatal(err)
	}
	tl := &docsTool{catalog: c}

	english, err := tl.search(context.Background(), "v1.19.5 usage statistics dashboard", "en", "user", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(english, "path=changelog/v1.19.5.md") || !strings.Contains(english, "source=release-notes/releases.json#v1.19.5") {
		t.Fatalf("English release search missing versioned changelog:\n%s", english)
	}

	chinese, err := tl.search(context.Background(), "v1.19.5 用量统计面板", "zh-CN", "user", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chinese, "path=changelog/v1.19.5.zh-CN.md") {
		t.Fatalf("Chinese release search missing versioned changelog:\n%s", chinese)
	}

	sections, err := tl.read("", "changelog/v1.19.5.zh-CN.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sections, "source: release-notes/releases.json#v1.19.5") {
		t.Fatalf("release section listing missing JSON provenance:\n%s", sections)
	}
	section, err := tl.read("changelog/v1.19.5.zh-CN.md::s002", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(section, "source: release-notes/releases.json#v1.19.5 rendered-lines=") {
		t.Fatalf("release section missing rendered provenance:\n%s", section)
	}
}

func TestReleaseSearchDoesNotTreatPreviewAsExactStableVersion(t *testing.T) {
	docsFS := fstest.MapFS{
		"GUIDE.md": {Data: []byte("# Guide\n\nEmbedded documentation.\n")},
	}
	releaseNotesFS := fstest.MapFS{
		"releases.json": {Data: []byte(`{
			"releases": [
				{"version":"1.19.0","title":{"en":"Stable","zh":"稳定版"},"summary":{"en":"Stable changelog","zh":"稳定版更新日志"}},
				{"version":"1.19.0-preview.1","title":{"en":"Preview","zh":"预览版"},"summary":{"en":"Preview-only changelog","zh":"仅预览版更新日志"}}
			]
		}`)},
	}
	c, err := loadCatalogWithReleaseNotes(docsFS, releaseNotesFS)
	if err != nil {
		t.Fatal(err)
	}
	tl := &docsTool{catalog: c}

	stable, err := tl.search(context.Background(), "1.19.0 changelog", "en", "user", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stable, "path=changelog/v1.19.0.md") {
		t.Fatalf("stable release search missing exact changelog:\n%s", stable)
	}
	if strings.Contains(stable, "path=changelog/v1.19.0-preview.1.md") {
		t.Fatalf("stable release search treated preview as an exact version match:\n%s", stable)
	}

	preview, err := tl.search(context.Background(), "1.19.0-preview.1 changelog", "en", "user", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "path=changelog/v1.19.0-preview.1.md") {
		t.Fatalf("preview release search missing exact changelog:\n%s", preview)
	}
}

func TestSourceManifestChangesWithReleaseCatalogBytes(t *testing.T) {
	docsFS := fstest.MapFS{"GUIDE.md": {Data: []byte("# Guide\n\nBody.\n")}}
	firstNotes := fstest.MapFS{"releases.json": {Data: []byte(`{"releases":[{"version":"1.0.0","title":{"en":"First","zh":"第一"},"summary":{"en":"Body","zh":"正文"}}]}`)}}
	secondNotes := fstest.MapFS{"releases.json": {Data: []byte(`{"releases":[{"version":"1.0.0","title":{"en":"Second","zh":"第二"},"summary":{"en":"Body","zh":"正文"}}]}`)}}
	first, err := SourceManifest(docsFS, firstNotes)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SourceManifest(docsFS, secondNotes)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == second.Digest || first.ReleaseNotes != 1 || second.ReleaseNotes != 1 {
		t.Fatalf("release catalog was not bound into manifests: first=%#v second=%#v", first, second)
	}
}

func TestReleaseCatalogRejectsUnsafeVirtualPathVersion(t *testing.T) {
	_, _, err := renderReleaseDocuments([]byte(`{"releases":[{"version":"../secret","title":{"en":"Bad","zh":"错误"},"summary":{"en":"Body","zh":"正文"}}]}`))
	if err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("unsafe release version error = %v", err)
	}
}

func TestSearchPrefersRelevantChineseAndEnglishSections(t *testing.T) {
	c, err := loadCatalog(productcontent.Content)
	if err != nil {
		t.Fatal(err)
	}
	tl := &docsTool{catalog: c}

	zh, err := tl.search(context.Background(), "工具权限 Auto Yolo 自动批准", "auto", "all", 5)
	if err != nil {
		t.Fatalf("Chinese search: %v", err)
	}
	if !strings.Contains(zh, "docs/TOOL_APPROVAL_MODES.zh-CN.md") {
		t.Fatalf("Chinese search did not find the permission guide:\n%s", zh)
	}

	en, err := tl.search(context.Background(), "REASONIX_HOME configuration paths", "auto", "all", 5)
	if err != nil {
		t.Fatalf("English search: %v", err)
	}
	if !strings.Contains(en, "docs/CONFIG_PATHS.md") {
		t.Fatalf("English search did not find configuration paths:\n%s", en)
	}
}

func TestToolSearchReadAndListRoundTrip(t *testing.T) {
	c, err := loadCatalog(fstest.MapFS{
		"GUIDE.md": {Data: []byte("# Guide\n\n## Configure MCP\n\nSet `reasonix.toml` before connecting plugins.\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	tl := &docsTool{catalog: c}
	search, err := tl.Execute(context.Background(), json.RawMessage(`{"operation":"search","query":"reasonix.toml MCP"}`))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(search, "GUIDE.md::s002") {
		t.Fatalf("search result missing stable section id:\n%s", search)
	}
	read, err := tl.Execute(context.Background(), json.RawMessage(`{"operation":"read","section_id":"GUIDE.md::s002"}`))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(read, "Set `reasonix.toml`") || !strings.Contains(read, "source: docs/GUIDE.md:3-5") {
		t.Fatalf("read result missing content or provenance:\n%s", read)
	}
	list, err := tl.Execute(context.Background(), json.RawMessage(`{"operation":"list","language":"en"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(list, "path=docs/GUIDE.md") {
		t.Fatalf("list result missing guide:\n%s", list)
	}
	for _, field := range []string{"version=", "revision=", "digest=sha256:"} {
		if !strings.Contains(list, field) {
			t.Fatalf("list result missing %s:\n%s", field, list)
		}
	}
}

func TestParserIgnoresHeadingsInsideFencedCode(t *testing.T) {
	doc := parseDocument("EXAMPLE.md", "# Example\n\n## Real\n\n```md\n### Not a section\n```\n\n    ### Indented code\n\nText about C#.\n")
	if len(doc.sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(doc.sections))
	}
	if !strings.Contains(doc.sections[1].content, "### Not a section") || !strings.Contains(doc.sections[1].content, "### Indented code") {
		t.Fatalf("code-block heading content was lost: %q", doc.sections[1].content)
	}
}

func TestGoldmarkHeadingsPreserveTextAndSetextSemantics(t *testing.T) {
	doc := parseDocument("EXAMPLE.md", "# Example\n\n### Configure *MCP* with `reasonix.toml`, C#, and <https://example.com>\n\nBody.\n\nSetext section\n--------------\n\nMore.\n")
	if len(doc.sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(doc.sections))
	}
	if doc.sections[1].heading != "Example > Configure MCP with reasonix.toml, C#, and https://example.com" {
		t.Fatalf("ATX heading = %q", doc.sections[1].heading)
	}
	if doc.sections[2].heading != "Example > Setext section" {
		t.Fatalf("Setext heading = %q", doc.sections[2].heading)
	}
}

func TestReadRejectsUntrustedPathsAndUnknownSections(t *testing.T) {
	c, err := loadCatalog(fstest.MapFS{
		"GUIDE.md": {Data: []byte("# Guide\n\nSafe content.\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	tl := &docsTool{catalog: c}
	if _, err := tl.read("../../secret::s001", ""); err == nil {
		t.Fatal("unknown section id should be rejected")
	}
	if _, err := tl.read("", "../../secret.md"); err == nil {
		t.Fatal("path traversal should be rejected")
	}
}

func TestCatalogRejectsInvalidUTF8(t *testing.T) {
	_, err := loadCatalog(fstest.MapFS{"BAD.md": {Data: []byte{0xff, 0xfe}}})
	if err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
}

func TestDocsToolContractIsStableAndReadOnly(t *testing.T) {
	tl := NewTool()
	if tl.Name() != "docs" || !tl.ReadOnly() {
		t.Fatalf("tool contract = name %q readOnly %v", tl.Name(), tl.ReadOnly())
	}
	canonical := provider.CanonicalizeSchema(tl.Schema())
	if !json.Valid(canonical) {
		t.Fatalf("docs schema is invalid or unstable: %s", canonical)
	}
	contract := tl.Name() + "\n" + tl.Description() + "\n" + string(canonical)
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(contract)))
	const want = "0113a592b6bcba5dd2be78c552f95ca27337534b6bb55b7b5e6fd89414f95be5"
	if got != want {
		t.Fatalf("provider-visible docs contract changed: got %s, want %s", got, want)
	}
}

func TestDocsToolSupportsConcurrentReadOnlyCalls(t *testing.T) {
	tl := NewTool()
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			args := json.RawMessage(`{"operation":"search","query":"MCP configuration","limit":2}`)
			if index%2 == 1 {
				args = json.RawMessage(`{"operation":"list","language":"zh-CN","audience":"user"}`)
			}
			out, err := tl.Execute(context.Background(), args)
			if err != nil {
				errs <- err
				return
			}
			if !strings.Contains(out, "digest=sha256:") {
				errs <- fmt.Errorf("missing corpus digest: %s", out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
