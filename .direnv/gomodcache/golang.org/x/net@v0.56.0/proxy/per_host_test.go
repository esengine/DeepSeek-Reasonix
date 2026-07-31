// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"testing"
)

type recordingProxy struct {
	addrs []string
}

func (r *recordingProxy) Dial(network, addr string) (net.Conn, error) {
	r.addrs = append(r.addrs, addr)
	return nil, errors.New("recordingProxy")
}

func TestPerHost(t *testing.T) {
	for _, test := range []struct {
		config  string   // passed to PerHost.AddFromString
		nomatch []string // addrs using the default dialer
		match   []string // addrs using the bypass dialer
	}{{
		config: "localhost,*.zone,127.0.0.1,10.0.0.1/8,1000::/16",
		nomatch: []string{
			"example.com:123",
			"1.2.3.4:123",
			"[1001::]:123",
		},
		match: []string{
			"localhost:123",
			"zone:123",
			"foo.zone:123",
			"127.0.0.1:123",
			"10.1.2.3:123",
			"[1000::]:123",
			"[1000::%25.example.com]:123",
		},
	}, {
		config: "localhost",
		nomatch: []string{
			"127.0.0.1:80",
		},
		match: []string{
			"localhost:80",
		},
	}, {
		config: "*.zone",
		nomatch: []string{
			"foo.com:80",
		},
		match: []string{
			"foo.zone:80",
			"foo.bar.zone:80",
		},
	}, {
		config: "1.2.3.4",
		nomatch: []string{
			"127.0.0.1:80",
			"11.2.3.4:80",
		},
		match: []string{
			"1.2.3.4:80",
		},
	}, {
		config: "10.0.0.0/24",
		nomatch: []string{
			"10.0.1.1:80",
		},
		match: []string{
			"10.0.0.1:80",
			"10.0.0.255:80",
		},
	}, {
		config: "fe80::/10",
		nomatch: []string{
			"[fec0::1]:80",
			"[fec0::1%en0]:80",
		},
		match: []string{
			"[fe80::1]:80",
			"[fe80::1%en0]:80",
		},
	}, {
		// We don't allow zone IDs in network prefixes,
		// so this config matches nothing.
		config: "fe80::%en0/10",
		nomatch: []string{
			"[fec0::1]:80",
			"[fec0::1%en0]:80",
			"[fe80::1]:80",
			"[fe80::1%en0]:80",
			"[fe80::1%en1]:80",
		},
	}} {
		for _, addr := range test.match {
			testPerHost(t, test.config, addr, true)
		}
		for _, addr := range test.nomatch {
			testPerHost(t, test.config, addr, false)
		}
	}
}

func testPerHost(t *testing.T, config, addr string, wantMatch bool) {
	name := fmt.Sprintf("config %q, dial %q", config, addr)

	var def, bypass recordingProxy
	perHost := NewPerHost(&def, &bypass)
	perHost.AddFromString(config)
	perHost.Dial("tcp", addr)

	// Dial and DialContext should have the same results.
	var defc, bypassc recordingProxy
	perHostc := NewPerHost(&defc, &bypassc)
	perHostc.AddFromString(config)
	perHostc.DialContext(context.Background(), "tcp", addr)
	if !slices.Equal(def.addrs, defc.addrs) {
		t.Errorf("%v: Dial default=%v, bypass=%v; DialContext default=%v, bypass=%v", name, def.addrs, bypass.addrs, defc.addrs, bypass.addrs)
		return
	}

	if got, want := slices.Concat(def.addrs, bypass.addrs), []string{addr}; !slices.Equal(got, want) {
		t.Errorf("%v: dialed %q, want %q", name, got, want)
		return
	}

	gotMatch := len(bypass.addrs) > 0
	if gotMatch != wantMatch {
		t.Errorf("%v: matched=%v, want %v", name, gotMatch, wantMatch)
		return
	}
}
