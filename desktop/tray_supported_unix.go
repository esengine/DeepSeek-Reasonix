//go:build !windows && !darwin

package main

import "github.com/godbus/dbus/v5"

func traySupported() bool {
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return false
	}
	defer conn.Close()
	return conn.Auth(nil) == nil && conn.Hello() == nil
}
