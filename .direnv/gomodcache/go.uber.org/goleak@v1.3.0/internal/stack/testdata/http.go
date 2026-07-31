//go:build ignore

package main

import (
	"fmt"
	"net"
	"net/http"
	"runtime"
	"time"
)

func main() {
	if err := start(); err != nil {
		panic(err)
	}

	fmt.Println(string(getStackBuffer()))
}

func start() error {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}

	go http.Serve(ln, nil)

	// Wait until HTTP server is ready.
	url := "http://" + ln.Addr().String()
	for i := 0; i < 10; i++ {
		if _, err := http.Get(url); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("failed to start HTTP server")
}

func getStackBuffer() []byte {
	for i := 4096; ; i *= 2 {
		buf := make([]byte, i)
		if n := runtime.Stack(buf, true /* all */); n < i {
			return buf[:n]
		}
	}
}
