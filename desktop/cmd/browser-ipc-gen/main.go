// Command browser-ipc-gen generates the Browser Companion protocol artifacts
// from desktop/internal/browseripc/schema.json. Run without flags to write the
// committed TypeScript artifact; pass -check (used by CI) to verify the
// committed files are current without modifying them.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"reasonix/desktop/internal/browseripc/gen"
)

const typeScriptArtifact = "browser-companion/src/generated/browserProtocol.generated.ts"

func main() {
	check := flag.Bool("check", false, "compare the generated artifact without writing it")
	root := flag.String("root", ".", "desktop module root containing generated artifacts")
	flag.Parse()
	log.SetFlags(0)

	ts, err := gen.GenerateTypeScript()
	if err != nil {
		log.Fatal(err)
	}
	path := filepath.Join(*root, typeScriptArtifact)
	if *check {
		current, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		if string(current) != string(ts) {
			log.Fatal("browser protocol TypeScript artifact is stale; run cmd/browser-ipc-gen")
		}
		fmt.Println("Browser protocol artifacts are up to date.")
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, ts, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println(path)
}
