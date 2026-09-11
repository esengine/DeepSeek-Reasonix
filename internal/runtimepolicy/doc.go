// Package runtimepolicy is the monotonic pre/post execution guard engine.
// Guards may only add Deny, Ask, Allow, or obligations; they never revoke a
// stronger decision, rewrite a resolved tool identity, or wait on I/O while
// the contract lock is held.
package runtimepolicy
