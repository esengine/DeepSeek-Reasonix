// Command toml-test-encoder satisfies the toml-test interface for testing TOML
// encoders. Namely, it accepts JSON on stdin and outputs TOML on stdout.
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
	log.Printf("Usage: %s < json-file\n", path.Base(os.Args[0]))
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	if flag.NArg() != 0 {
		flag.Usage()
	}

	var tmp any
	if err := json.NewDecoder(os.Stdin).Decode(&tmp); err != nil {
		log.Fatalf("Error decoding JSON: %s", err)
	}

	rm, err := tag.Remove(tmp)
	if err != nil {
		log.Fatalf("Error decoding JSON: %s", err)
	}

	if err := toml.NewEncoder(os.Stdout).Encode(rm); err != nil {
		log.Fatalf("Error encoding TOML: %s", err)
	}
}
