package main

import (
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

type foo string

func (f foo) Foo() (string, *dbus.Error) {
	fmt.Println(f)
	return string(f), nil
}

type Foo struct {
	Id    int
	Value string
}

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	reply, err := conn.RequestName("com.github.guelfey.Demo",
		dbus.NameFlagDoNotQueue)
	if err != nil {
		panic(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		_, _ = fmt.Fprintln(os.Stderr, "name already taken")
		os.Exit(1)
	}
	propsSpec := map[string]map[string]*prop.Prop{
		"com.github.guelfey.Demo": {
			"SomeInt": {
				int32(0),
				true,
				prop.EmitTrue,
				func(c *prop.Change) *dbus.Error {
					fmt.Println(c.Name, "changed to", c.Value)
					return nil
				},
			},
			"FooStruct": {
				Foo{Id: 1, Value: "First"},
				true,
				prop.EmitTrue,
				func(c *prop.Change) *dbus.Error {
					var foo Foo
					err := dbus.Store([]interface{}{c.Value}, &foo)
					if err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "dbus.Store foo failed: %v\n", err)
					}
					fmt.Println(c.Name, "changed to", foo)
					return nil
				},
			},
		},
	}
	f := foo("Bar")
	err = conn.Export(f, "/com/github/guelfey/Demo", "com.github.guelfey.Demo")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "export f failed: %v\n", err)
		os.Exit(1)
	}
	props, err := prop.Export(conn, "/com/github/guelfey/Demo", propsSpec)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "export propsSpec failed: %v\n", err)
		os.Exit(1)
	}
	n := &introspect.Node{
		Name: "/com/github/guelfey/Demo",
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       "com.github.guelfey.Demo",
				Methods:    introspect.Methods(f),
				Properties: props.Introspection("com.github.guelfey.Demo"),
			},
		},
	}
	err = conn.Export(introspect.NewIntrospectable(n), "/com/github/guelfey/Demo",
		"org.freedesktop.DBus.Introspectable")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "export introspect failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Listening on com.github.guelfey.Demo / /com/github/guelfey/Demo ...")

	c := make(chan *dbus.Signal)
	conn.Signal(c)
	for range c {
	}
}
