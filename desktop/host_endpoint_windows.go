//go:build windows

package main

import (
	"os"
	"reasonix/desktop/internal/instanceidentity"
	"reasonix/internal/config"
)

func startUpdateEndpoint() (func(), error) {
	if !hostRPCRequested(os.Args[1:]) {
		return func() {}, nil
	}
	return instanceidentity.ListenEndpoint(instanceidentity.ForHome(config.ReasonixHomeDir()))
}
