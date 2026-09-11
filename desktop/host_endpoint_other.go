//go:build !windows

package main

func startUpdateEndpoint() (func(), error) { return func() {}, nil }
