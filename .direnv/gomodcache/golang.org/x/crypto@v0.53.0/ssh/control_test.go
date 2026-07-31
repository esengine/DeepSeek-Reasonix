// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestControlClientHandshake(t *testing.T) {
	reqs := [][]byte{
		// Hello request.
		{0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x04},
		// Client proxy request.
		{0x00, 0x00, 0x00, 0x08, 0x10, 0x00, 0x00, 0x0f, 0x00, 0x00, 0x00, 0x00},
	}
	respsNormal := [][]byte{
		// Hello response.
		{0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x04},
		// Server proxy response.
		{0x00, 0x00, 0x00, 0x08, 0x80, 0x00, 0x00, 0x0f, 0x00, 0x00, 0x00, 0x00},
	}
	for _, tt := range []struct {
		name        string
		resps       [][]byte
		expectedErr string
	}{
		{
			name:  "normal handshake",
			resps: respsNormal,
		},
		{
			name: "length greater than max",
			resps: [][]byte{
				{0xff, 0xff, 0xff, 0xff},
				respsNormal[1],
			},
			expectedErr: "message length 4294967295 exceeds maximum",
		},
		{
			name: "missing hello response",
			resps: [][]byte{
				{},
			},
			expectedErr: "use of closed network connection",
		},
		{
			name: "hello response too short",
			resps: [][]byte{
				{0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00},
				respsNormal[1],
			},
			expectedErr: "EOF",
		},
		{
			name: "bad hello response type",
			resps: [][]byte{
				{0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04},
				respsNormal[1],
			},
			expectedErr: "expected hello response, got 0",
		},
		{
			name: "bad protocol version",
			resps: [][]byte{
				{0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00},
				respsNormal[1],
			},
			expectedErr: "mux server has unsupported version 0",
		},
		{
			name: "missing server proxy response",
			resps: [][]byte{
				respsNormal[0],
			},
			expectedErr: "use of closed network connection",
		},
		{
			name: "server proxy response too short",
			resps: [][]byte{
				respsNormal[0],
				{0x00, 0x00, 0x00, 0x06, 0x80, 0x00, 0x00, 0x0f, 0x00, 0x00},
			},
			expectedErr: "EOF",
		},
		{
			name: "bad server proxy response type",
			resps: [][]byte{
				respsNormal[0],
				{0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			},
			expectedErr: "expected server proxy response, got 0",
		},
		{
			name: "bad request id",
			resps: [][]byte{
				respsNormal[0],
				{0x00, 0x00, 0x00, 0x08, 0x80, 0x00, 0x00, 0x0f, 0x00, 0x00, 0x00, 0x01},
			},
			expectedErr: "expected request id 0, got 1",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			done := make(chan error, 1)

			ok := func() bool {
				c1, c2, err := netPipe()
				if err != nil {
					t.Fatalf("netPipe: %v", err)
				}
				defer c1.Close()
				defer c2.Close()

				go func() {
					defer close(done)
					_, _, _, err := NewControlClientConn(c2)
					c2.Write([]byte{0}) // Dummy message to unblock the final read.
					done <- err
				}()

				i := 0
				for ; i < len(reqs) && i < len(tt.resps); i++ {
					expected := reqs[i]
					buf := make([]byte, len(expected))
					if _, err := io.ReadFull(c1, buf); err != nil {
						t.Errorf("error reading message %d: %v", i+1, err)
						return false
					}
					if !bytes.Equal(buf, expected) {
						t.Errorf(
							"unexpected message %d: got %v, want %v",
							i+1, buf, expected,
						)
						return false
					}
					_, err = c1.Write(tt.resps[i])
					if err != nil {
						t.Errorf("error writing message %d: %v", i+1, err)
						return false
					}
				}
				// Wait for the next message so that the final response can be read.
				buf := make([]byte, 1)
				c1.Read(buf)
				return true
			}()
			if !ok {
				return
			}

			err := <-done
			if tt.expectedErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectedErr) {
					t.Fatalf("got err %q; want err containing %q", err, tt.expectedErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("got err %q; want no err", err)
			}
		})
	}
}

func TestControlClientTransport(t *testing.T) {
	type response struct {
		status  bool
		payload []byte
		err     error
	}

	for _, tt := range []struct {
		name        string
		resp        []byte
		respStatus  bool
		respPayload []byte
		expectedErr string
	}{
		{
			name:       "successful request",
			resp:       []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x51},
			respStatus: true,
		},
		{
			name:       "failed request",
			resp:       []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x52},
			respStatus: false,
		},
		{
			name:        "short response",
			resp:        []byte{0x00, 0x00, 0x00, 0x00},
			expectedErr: "EOF",
		},
		{
			name:        "response with payload",
			resp:        []byte{0x00, 0x00, 0x00, 0x05, 0x00, 0x51, 0x01, 0x02, 0x03},
			respStatus:  true,
			respPayload: []byte{1, 2, 3},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c1, c2, err := netPipe()
			if err != nil {
				t.Fatalf("netPipe: %v", err)
			}
			defer c1.Close()
			defer c2.Close()

			// Handshake responses.
			c1.Write([]byte{
				0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x04,
				0x00, 0x00, 0x00, 0x08, 0x80, 0x00, 0x00, 0x0f, 0x00, 0x00, 0x00, 0x00,
			})

			conn, chans, reqs, err := NewControlClientConn(c2)
			if err != nil {
				t.Fatal(err)
			}
			client := NewClient(conn, chans, reqs)

			done := make(chan response, 1)
			go func() {
				defer close(done)
				status, payload, err := client.SendRequest("hello", true, nil)
				if err != nil {
					done <- response{err: err}
					return
				}
				done <- response{
					status:  status,
					payload: payload,
				}
			}()

			// Discard handshake.
			io.CopyN(io.Discard, c1, 24)

			expectedReq := []byte{
				0x00, 0x00, 0x00, 0x0c, 0x00, 0x50,
				0x00, 0x00, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o',
				0x01,
			}
			buf := make([]byte, len(expectedReq))
			if _, err := io.ReadFull(c1, buf); err != nil {
				t.Fatalf("reading request: %v", err)
			}
			if !bytes.Equal(buf, expectedReq) {
				t.Fatalf("got request %v; want %v", buf, expectedReq)
			}

			c1.Write(tt.resp)

			resp := <-done
			if tt.expectedErr != "" {
				if resp.err == nil || !strings.Contains(resp.err.Error(), tt.expectedErr) {
					t.Fatalf("got err %q; want err containing %q", resp.err, tt.expectedErr)
				}
				return
			}
			if resp.err != nil {
				t.Fatalf("got err %q; want no err", resp.err)
			}
			if resp.status != tt.respStatus {
				t.Fatalf("got status %v; want %v", resp.status, tt.respStatus)
			}
			if !bytes.Equal(resp.payload, tt.respPayload) {
				t.Errorf("got payload %v; want %v", resp.payload, tt.respPayload)
			}
		})
	}
}
