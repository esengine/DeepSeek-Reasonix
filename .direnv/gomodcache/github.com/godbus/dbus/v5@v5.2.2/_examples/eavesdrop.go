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

	for _, v := range []string{"method_call", "method_return", "error", "signal"} {
		call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
			"eavesdrop='true',type='"+v+"'")
		if call.Err != nil {
			fmt.Fprintln(os.Stderr, "Failed to add match:", call.Err)
			os.Exit(1)
		}
	}
	c := make(chan *dbus.Message, 10)
	conn.Eavesdrop(c)
	fmt.Println("Listening for everything")
	for v := range c {
		fmt.Println(v)
	}
}
