package main

import (
	"fmt"

	"example.com/toolkit/farewell"
	"example.com/toolkit/greet"
)

func main() {
	fmt.Println(greet.Hello("world"))
	fmt.Println(farewell.Bye("world"))
}
