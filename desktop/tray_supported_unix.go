//go:build !windows && !darwin

package main

import "github.com/godbus/dbus/v5"

func traySupported() bool {
	// SessionBus() returns a shared singleton connection — do NOT close it.
	// Closing the shared connection can interfere with other DBus consumers
	// (notifications, portal dialogs) in the same process.
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	var owner string
	return obj.Call("org.freedesktop.DBus.GetNameOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owner) == nil && owner != ""
}
