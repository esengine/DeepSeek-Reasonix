//go:build !windows && !darwin

package main

import "github.com/godbus/dbus/v5"

func traySupported() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	var owner string
	return obj.Call("org.freedesktop.DBus.GetNameOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owner) == nil && owner != ""
}
