package dbus

import "fmt"

func ExampleConn_Emit() {
	conn, err := ConnectSystemBus()
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if err := conn.Emit("/foo/bar", "foo.bar.Baz", uint32(0xDAEDBEEF)); err != nil {
		panic(err)
	}
}

func ExampleObject_Call() {
	var list []string

	conn, err := ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	err = conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&list)
	if err != nil {
		panic(err)
	}
	for _, v := range list {
		fmt.Println(v)
	}
}

func ExampleObject_Go() {
	conn, err := ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	ch := make(chan *Call, 10)
	conn.BusObject().Go("org.freedesktop.DBus.ListActivatableNames", 0, ch)
	call := <-ch
	if call.Err != nil {
		panic(err)
	}
	list := call.Body[0].([]string)
	for _, v := range list {
		fmt.Println(v)
	}
}
