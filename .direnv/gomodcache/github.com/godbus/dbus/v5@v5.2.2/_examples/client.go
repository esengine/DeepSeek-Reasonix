package main

import (
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
)

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to connect to session bus:", err)
		os.Exit(1)
	}
	defer conn.Close()

	var s string
	obj := conn.Object("com.github.guelfey.Demo", "/com/github/guelfey/Demo")
	err = obj.Call("com.github.guelfey.Demo.Foo", 0).Store(&s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to call Foo function (is the server example running?):", err)
		os.Exit(1)
	}

	fmt.Println("Result from calling Foo function on com.github.guelfey.Demo interface:")
	fmt.Println(s)
}
