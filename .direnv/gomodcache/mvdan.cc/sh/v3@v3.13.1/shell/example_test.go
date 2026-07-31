// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell_test

import (
	"fmt"

	"mvdan.cc/sh/v3/shell"
)

func ExampleExpand() {
	env := func(name string) string {
		switch name {
		case "HOME":
			return "/home/user"
		}
		return "" // leave the rest unset
	}
	out, _ := shell.Expand("No place like $HOME", env)
	fmt.Println(out)

	out, _ = shell.Expand("Some vars are ${missing:-awesome}", env)
	fmt.Println(out)

	out, _ = shell.Expand("Math is fun! $((12 * 34))", nil)
	fmt.Println(out)
	// Output:
	// No place like /home/user
	// Some vars are awesome
	// Math is fun! 408
}

func ExampleFields() {
	env := func(name string) string {
		switch name {
		case "foo":
			return "bar baz"
		}
		return "" // leave the rest unset
	}
	out, _ := shell.Fields(`"many quoted" ' strings '`, env)
	fmt.Printf("%#v\n", out)

	out, _ = shell.Fields("unquoted $foo", env)
	fmt.Printf("%#v\n", out)

	out, _ = shell.Fields(`quoted "$foo"`, env)
	fmt.Printf("%#v\n", out)
	// Output:
	// []string{"many quoted", " strings "}
	// []string{"unquoted", "bar", "baz"}
	// []string{"quoted", "bar baz"}
}
