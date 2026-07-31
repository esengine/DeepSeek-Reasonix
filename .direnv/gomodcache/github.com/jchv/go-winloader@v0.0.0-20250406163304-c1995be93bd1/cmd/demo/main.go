package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/jchv/go-winloader"
)

func main() {
	flag.Parse()
	data, err := ioutil.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatalln(err)
	}
	module, err := winloader.LoadFromMemory(data)
	if err != nil {
		log.Fatalln(err)
	}
	proc := module.Proc("Add")
	if proc == nil {
		log.Fatalln("could not find proc Add")
	}
	r, _, _ := proc.Call(1, 2)
	fmt.Printf("Add(1, 2) = %d\n", r)
}
