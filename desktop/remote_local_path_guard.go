package main

import "errors"

// ErrRemoteLocalPathOperation is returned when a Desktop-native opener is
// asked to consume a Host-owned path while the Remote target is selected.
// Remote display paths are presentation-only and must never be interpreted by
// the Desktop filesystem or shell.
var ErrRemoteLocalPathOperation = errors.New("Remote Host paths cannot be opened by Desktop local path operations")
