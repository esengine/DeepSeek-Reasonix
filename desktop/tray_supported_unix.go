//go:build !windows && !darwin

package main

import "github.com/godbus/dbus/v5"

func traySupported() bool {
	// SessionBusPrivate() returns a private connection rather than the shared
	// singleton from SessionBus(). Closing a shared singleton connection can
	// interfere with other DBus users in the same process.
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := conn.Auth(); err != nil {
		return false
	}
	if err := conn.Hello(); err != nil {
		return false
	}
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	var owner string
	return obj.Call("org.freedesktop.DBus.GetNameOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owner) == nil && owner != ""
}
