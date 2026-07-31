package main

import (
	"fmt"

	"github.com/aymanbagabas/go-udiff"
)

func main() {
	a := "Hello, world!\n"
	b := "Hello, Go!\nSay hi to µDiff"

	edits := udiff.Strings(a, b)
	d, err := udiff.ToUnifiedDiff("a.txt", "b.txt", a, edits, udiff.DefaultContextLines)
	if err != nil {
		panic(err)
	}

	for _, h := range d.Hunks {
		fmt.Printf("hunk: -%d, +%d\n", h.FromLine, h.ToLine)
		for _, l := range h.Lines {
			fmt.Printf("%s %q\n", l.Kind, l.Content)
		}
	}
}
