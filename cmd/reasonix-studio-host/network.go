package main

import (
	"os"

	"reasonix/internal/base/netdns"
)

func init() {
	netdns.Install(netdns.Parse(os.Getenv(netdns.EnvVar)))
}
