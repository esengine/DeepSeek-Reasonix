// Command toml-test-decoder satisfies the toml-test interface for testing TOML
// decoders. Namely, it accepts TOML on stdin and outputs JSON on stdout.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"path"

	"github.com/BurntSushi/toml"
	"github.com/BurntSushi/toml/internal/tag"
)

func init() {
	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()
}

func usage() {
	log.Printf("Usage: %s < toml-file\n", path.Base(os.Args[0]))
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	if flag.NArg() != 0 {
		flag.Usage()
	}

	var decoded any
	if _, err := toml.NewDecoder(os.Stdin).Decode(&decoded); err != nil {
		log.Fatalf("Error decoding TOML: %s", err)
	}

	j := json.NewEncoder(os.Stdout)
	j.SetIndent("", "  ")
	if err := j.Encode(tag.Add("", decoded)); err != nil {
		log.Fatalf("Error encoding JSON: %s", err)
	}
}
