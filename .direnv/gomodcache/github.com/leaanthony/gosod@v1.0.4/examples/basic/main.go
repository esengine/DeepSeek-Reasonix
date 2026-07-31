package main

import (
	"log"
	"os"

	"github.com/leaanthony/gosod"
)

type config struct {
	Name string
}

func main() {
	fs := os.DirFS("./myTemplate")

	// Define a new Template directory
	basic := gosod.New(fs)

	// Make some config data
	myConfig := &config{
		Name: "Mat",
	}

	// Create a new directory using the template and config
	err := basic.Extract("./generated", myConfig)
	if err != nil {
		log.Fatal(err)
	}
}
