package main

import (
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to connect to session bus:", err)
		os.Exit(1)
	}
	defer conn.Close()

	ch := make(chan *dbus.Call, 10)

	obj := conn.Object("com.github.guelfey.Demo", "/com/github/guelfey/Demo")
	obj.Go("com.github.guelfey.Demo.Sleep", 0, ch, 5) // 5 seconds

	nrAttempts := 3
	isResponseReceived := false

	for i := 1; i <= nrAttempts && !isResponseReceived; i++ {
		fmt.Println("Waiting for response, attempt", i)

		select {
		case call := <-ch:
			if call.Err != nil {
				fmt.Fprintln(os.Stderr, "Failed to call Sleep method:", err)
				os.Exit(1)
			}
			isResponseReceived = true
			break

		// Handle timeout here
		case <-time.After(2 * time.Second):
			fmt.Println("Timeout")
			break
		}
	}

	if isResponseReceived {
		fmt.Println("Done!")
	} else {
		fmt.Fprintln(os.Stderr, "Timeout waiting for Sleep response")
	}
}
