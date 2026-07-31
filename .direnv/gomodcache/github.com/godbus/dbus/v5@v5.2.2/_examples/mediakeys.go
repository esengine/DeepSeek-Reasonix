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

	if err = conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/gnome/SettingsDaemon"),
		dbus.WithMatchInterface("org.gnome.SettingsDaemon.MediaKeys"),
	); err != nil {
		panic(err)
	}

	// Grab media player keys.
	bus := conn.Object("org.gnome.SettingsDaemon", "/org/gnome/SettingsDaemon/MediaKeys")
	call := bus.Call("org.gnome.SettingsDaemon.MediaKeys.GrabMediaPlayerKeys", 0, "test app", uint(0))
	if call.Err != nil {
		panic(err)
	}

	signals := make(chan *dbus.Signal, 10)
	conn.Signal(signals)

	for {
		select {
		case message := <-signals:
			fmt.Println("Message:", message)
		}
	}
}
