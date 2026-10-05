package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"rsc.io/qr"

	"reasonix/internal/frontend/serve"
)

func lan() []serve.ShareAddress {
	return []serve.ShareAddress{{Interface: "eth0", IP: "192.168.1.20", Kind: serve.AddressLAN}}
}

func TestShareBaseNamesWhatAPhoneCanOpen(t *testing.T) {
	cases := []struct {
		public, bind, want string
		err                error
	}{
		{bind: "0.0.0.0:8787", want: "http://192.168.1.20:8787/"},
		{bind: ":8787", want: "http://192.168.1.20:8787/"},
		{bind: "10.0.0.4:8787", want: "http://10.0.0.4:8787/"},
		{bind: "203.0.113.9:8787", want: "http://203.0.113.9:8787/"},
		{public: "https://agent.example.com/some/path", bind: "127.0.0.1:8787", want: "https://agent.example.com/"},
		{bind: "127.0.0.1:8787", err: errShareLoopback},
		{bind: "localhost:8787", err: errShareLoopback},
		{public: "ftp://agent.example.com", bind: "0.0.0.0:8787", err: errShareBadURL},
	}
	for _, c := range cases {
		u, err := shareBase(c.public, c.bind, lan)
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Fatalf("shareBase(%q, %q) err = %v, want %v", c.public, c.bind, err, c.err)
			}
			continue
		}
		if err != nil || u.String() != c.want {
			t.Fatalf("shareBase(%q, %q) = %v, %v; want %s", c.public, c.bind, u, err, c.want)
		}
	}
	if _, err := shareBase("", "0.0.0.0:8787", func() []serve.ShareAddress { return nil }); !errors.Is(err, errShareNoAddress) {
		t.Fatalf("wildcard bind with no private address err = %v, want errShareNoAddress", err)
	}
}

// The token rides the link's fragment, so only a link that is encrypted or
// never leaves a private network may carry it.
func TestTheTokenRidesOnlyALinkThatStaysOffTheInternet(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://agent.example.com/": true,
		"https://203.0.113.9:8787/":  true,
		"http://192.168.1.20:8787/":  true,
		"http://100.101.7.3:8787/":   true,
		"http://203.0.113.9:8787/":   false,
		"http://agent.example.com/":  false,
	} {
		u, err := shareBase(raw, "0.0.0.0:1", lan)
		if err != nil {
			t.Fatal(err)
		}
		if got := credentialMayRide(u); got != want {
			t.Fatalf("credentialMayRide(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestAPublicPlainHTTPServerDrawsNoCodeForItsToken(t *testing.T) {
	var out bytes.Buffer
	reportShareQR(&out, "token", "sekret", "203.0.113.9:8787", "")
	if strings.Contains(out.String(), "sekret") || strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("output carries the token or a code:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--public-url https://") {
		t.Fatalf("refusal does not say what to do:\n%s", out.String())
	}
}

func TestAPrivateServerDrawsItsCode(t *testing.T) {
	var out bytes.Buffer
	reportShareQR(&out, "token", "sekret", "192.168.1.20:8787", "")
	if !strings.Contains(out.String(), "scan to open http://192.168.1.20:8787/") || !strings.Contains(out.String(), "█") {
		t.Fatalf("no code drawn:\n%s", out.String())
	}
	out.Reset()
	reportShareQR(&out, "none", "", "192.168.1.20:8787", "")
	if out.Len() != 0 {
		t.Fatalf("a server with no auth drew %q; an open link needs no code", out.String())
	}
}

// Reading the half blocks back must give the symbol module for module, quiet
// zone included, or what a phone scans is some other code.
func TestTheTerminalCodeReadsBackAsTheSymbol(t *testing.T) {
	code, err := qr.Encode("http://192.168.1.20:8787/#token=abc", qr.L)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writeQR(&out, code)
	side := code.Size + 2*qrQuiet
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != (side+1)/2 {
		t.Fatalf("%d lines, want %d", len(lines), (side+1)/2)
	}
	for row, line := range lines {
		line = strings.TrimSuffix(strings.TrimPrefix(line, "  \x1b[30;107m"), "\x1b[0m")
		cells := []rune(line)
		if len(cells) != side {
			t.Fatalf("line %d has %d cells, want %d", row, len(cells), side)
		}
		for x, c := range cells {
			top := c == '█' || c == '▀'
			bottom := c == '█' || c == '▄'
			for dy, got := range []bool{top, bottom} {
				y := 2*row + dy
				mx, my := x-qrQuiet, y-qrQuiet
				want := mx >= 0 && my >= 0 && mx < code.Size && my < code.Size && code.Black(mx, my)
				if got != want {
					t.Fatalf("module (%d,%d) dark=%v, want %v", x, y, got, want)
				}
			}
		}
	}
}
