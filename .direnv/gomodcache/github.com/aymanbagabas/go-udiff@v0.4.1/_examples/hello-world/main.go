package main

import (
	"fmt"

	"github.com/aymanbagabas/go-udiff"
)

func main() {
	a := "Hello, world!\n"
	b := "Hello, Go!\nSay hi to µDiff"
	d := udiff.Unified("a.txt", "b.txt", a, b)
	fmt.Println(d)
}
