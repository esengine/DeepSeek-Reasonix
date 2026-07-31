// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.27 && !http2legacy

package http2

import "net/http"

func (t *Transport) TestTransport() *http.Transport {
	t.init()
	return t.t1
}

func (s *Server) TestSetNewConnFunc(f func(*ServerConn)) {
	panic("ServerConns are only available with http2legacy")
}
