// Command tomlv validates TOML documents and prints each key's type.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/BurntSushi/toml"
)

var (
	flagTypes = false
	flagJSON  = false
	flagTime  = false
)

func init() {
	log.SetFlags(0)
	flag.BoolVar(&flagTypes, "types", flagTypes, "Show the types for every key.")
	flag.BoolVar(&flagTime, "time", flagTypes, "Show how long the parsing took.")
	flag.BoolVar(&flagJSON, "json", flagTypes, "Output parsed document as JSON.")
	flag.Usage = usage
	flag.Parse()
}

func usage() {
	log.Printf("Usage: %s toml-file [ toml-file ... ]\n", path.Base(os.Args[0]))
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	if flag.NArg() < 1 || flag.Args()[0] == "-" {
		read("-", os.Stdin)
		return
	}

	for _, f := range flag.Args() {
		fp, err := os.Open(f)
		if err != nil {
			log.Fatalf("reading %q: %s", f, err)
		}
		read(f, fp)
		fp.Close()
	}
}

func read(f string, fp io.Reader) {
	// Read data first so -time doesn't include the time it took to read.
	d, err := io.ReadAll(fp)
	if err != nil {
		log.Fatalf("Error in '%s': %s", f, err)
	}

	var tmp any
	start := time.Now()
	md, err := toml.Decode(string(d), &tmp)
	if err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			log.Fatalf("Error in '%s': %s", f, perr.ErrorWithPosition())
		}
		log.Fatalf("Error in '%s': %s", f, err)
	}
	if flagTime {
		fmt.Printf("%f\n", time.Since(start).Seconds())
	}
	if flagTypes {
		printTypes(md)
	}
	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		enc.Encode(tmp)
	}
}

func printTypes(md toml.MetaData) {
	tabw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, key := range md.Keys() {
		fmt.Fprintf(tabw, "%s%s\t%s\n",
			strings.Repeat("    ", len(key)-1), key, md.Type(key...))
	}
	tabw.Flush()
}
