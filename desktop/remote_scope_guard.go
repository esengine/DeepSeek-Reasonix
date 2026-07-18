package main

import (
	"errors"
	"fmt"
)

var (
	ErrRemoteV1Deferred = errors.New("feature is deferred for Remote V1")
	ErrRemoteOutOfScope = errors.New("operation is out of scope for Remote V1")
)

func (a *App) rejectRemoteDeferred(method string) error {
	if !a.remoteTargetSelected() {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrRemoteV1Deferred, method)
}

func (a *App) rejectRemoteOutOfScope(method string) error {
	if !a.remoteTargetSelected() {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrRemoteOutOfScope, method)
}
