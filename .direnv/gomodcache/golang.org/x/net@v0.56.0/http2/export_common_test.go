// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package http2

import "time"

const (
	InitialHeaderTableSize = initialHeaderTableSize
	InitialWindowSize      = initialWindowSize
)

type (
	ServerConn = serverConn
)

func SummarizeFrame(f Frame) string {
	return summarizeFrame(f)
}

const GoAwayTimeout = 25 * time.Millisecond
