//go:build !linux

package service

import (
	"context"
	"net"
)

func DefaultEndpoint() (*Endpoint, error) { return nil, ErrUnsupportedPlatform }

func (e *Endpoint) Installed(context.Context) (bool, error) {
	return false, ErrUnsupportedPlatform
}

func (e *Endpoint) Dial(context.Context) (net.Conn, error) {
	return nil, ErrUnsupportedPlatform
}

func (e *Endpoint) Listen() (net.Listener, error) {
	return nil, ErrUnsupportedPlatform
}
