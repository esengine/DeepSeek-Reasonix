// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package route_test

import (
	"fmt"
	"net/netip"
	"os"
	"syscall"

	"golang.org/x/net/route"
	"golang.org/x/sys/unix"
)

// This example demonstrates how to parse a response to RTM_GET request.
func ExampleParseRIB() {
	fd, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, unix.AF_UNSPEC)
	if err != nil {
		return
	}
	defer unix.Close(fd)

	// Create a RouteMessage with RTM_GET type
	rtm := &route.RouteMessage{
		Version: syscall.RTM_VERSION,
		Type:    unix.RTM_GET,
		ID:      uintptr(os.Getpid()),
		Seq:     0,
		Addrs: []route.Addr{
			&route.Inet4Addr{IP: [4]byte{127, 0, 0, 0}},
		},
	}

	// Marshal the message into bytes
	msgBytes, err := rtm.Marshal()
	if err != nil {
		return
	}

	// Send the message over the routing socket
	_, err = unix.Write(fd, msgBytes)
	if err != nil {
		return
	}

	// Read the response from the routing socket
	var buf [2 << 10]byte
	n, err := unix.Read(fd, buf[:])
	if err != nil {
		return
	}

	// Parse the response messages
	msgs, err := route.ParseRIB(route.RIBTypeRoute, buf[:n])
	if err != nil {
		return
	}
	routeMsg, ok := msgs[0].(*route.RouteMessage)
	if !ok {
		return
	}
	netmask, ok := routeMsg.Addrs[2].(*route.Inet4Addr)
	if !ok {
		return
	}
	fmt.Println(netip.AddrFrom4(netmask.IP))
	// Output: 255.0.0.0
}
