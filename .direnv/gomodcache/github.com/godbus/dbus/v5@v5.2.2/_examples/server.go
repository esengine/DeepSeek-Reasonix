package main

import (
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const intro = `
<node>
	<interface name="com.github.guelfey.Demo">
		<method name="Foo">
			<arg direction="out" type="s"/>
		</method>
		<method name="Sleep">
			<arg direction="in" type="u"/>
		</method>
	</interface>` + introspect.IntrospectDataString + `</node> `

type foo string

func (f foo) Foo() (string, *dbus.Error) {
	fmt.Println(f)
	return string(f), nil
}

func (f foo) Sleep(seconds uint) *dbus.Error {
	fmt.Println("Sleeping", seconds, "second(s)")
	time.Sleep(time.Duration(seconds) * time.Second)
	return nil
}

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	f := foo("Bar!")
	conn.Export(f, "/com/github/guelfey/Demo", "com.github.guelfey.Demo")
	conn.Export(introspect.Introspectable(intro), "/com/github/guelfey/Demo",
		"org.freedesktop.DBus.Introspectable")

	reply, err := conn.RequestName("com.github.guelfey.Demo",
		dbus.NameFlagDoNotQueue)
	if err != nil {
		panic(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		fmt.Fprintln(os.Stderr, "name already taken")
		os.Exit(1)
	}
	fmt.Println("Listening on com.github.guelfey.Demo / /com/github/guelfey/Demo ...")
	select {}
}
