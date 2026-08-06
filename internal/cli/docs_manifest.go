package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/productdocs"
)

// docsManifestCommand is an intentionally undocumented release/diagnostic
// command. User-facing retrieval stays on /docs and the read-only docs tool;
// release jobs use this command to inspect the compiled corpus before publish.
func docsManifestCommand(args []string, cliVersion string) int {
	flags := flag.NewFlagSet("docs-manifest", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	verifySource := flags.String("verify-source", "", "compare the embedded corpus with a Reasonix source root")
	expectVersion := flags.String("expect-version", "", "require this embedded product version")
	expectRevision := flags.String("expect-revision", "", "require this embedded source revision")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "error: docs-manifest accepts flags only")
		return 2
	}

	manifest, err := productdocs.EmbeddedManifest()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: load embedded documentation:", err)
		return 1
	}
	if manifest.Version != strings.TrimSpace(cliVersion) {
		fmt.Fprintf(os.Stderr, "error: docs build version %q does not match CLI version %q\n", manifest.Version, cliVersion)
		return 1
	}
	if expected := strings.TrimSpace(*expectVersion); expected != "" && manifest.Version != expected {
		fmt.Fprintf(os.Stderr, "error: docs build version %q, expected %q\n", manifest.Version, expected)
		return 1
	}
	if expected := strings.TrimSpace(*expectRevision); expected != "" && manifest.Revision != expected {
		fmt.Fprintf(os.Stderr, "error: docs source revision %q, expected %q\n", manifest.Revision, expected)
		return 1
	}
	if sourceDir := strings.TrimSpace(*verifySource); sourceDir != "" {
		source, err := productdocs.SourceManifest(
			os.DirFS(filepath.Join(sourceDir, "docs")),
			os.DirFS(filepath.Join(sourceDir, "release-notes")),
		)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: load source documentation:", err)
			return 1
		}
		if manifest.Digest != source.Digest || manifest.Documents != source.Documents || manifest.Sections != source.Sections || manifest.ReleaseNotes != source.ReleaseNotes {
			fmt.Fprintf(os.Stderr, "error: embedded docs do not match %s: embedded=%s/%d/%d/%d source=%s/%d/%d/%d\n",
				sourceDir, manifest.Digest, manifest.Documents, manifest.Sections, manifest.ReleaseNotes,
				source.Digest, source.Documents, source.Sections, source.ReleaseNotes)
			return 1
		}
	}

	encoded, err := json.Marshal(manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: encode docs manifest:", err)
		return 1
	}
	fmt.Println(string(encoded))
	return 0
}
