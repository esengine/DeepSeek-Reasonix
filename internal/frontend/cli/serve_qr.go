package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"rsc.io/qr"

	"reasonix/internal/frontend/serve"
)

var (
	// errShareLoopback is a server only this machine can reach: a phone has
	// nothing to open.
	errShareLoopback = errors.New("listening on loopback only; bind a reachable address with --addr, or name one with --public-url")
	// errShareNoAddress is a wildcard bind on a machine with no private address
	// to put in its place.
	errShareNoAddress = errors.New("no reachable address to put in a code; name one with --public-url")
	// errShareBadURL is a --public-url that is not an http(s) origin.
	errShareBadURL = errors.New("--public-url must be an http:// or https:// address")
)

// shareBase is the origin a phone should open. A named --public-url wins,
// since only the operator knows what a proxy in front answers for; otherwise
// the bound address, with a wildcard bind replaced by this machine's first
// private address.
func shareBase(publicURL, bindAddr string, private func() []serve.ShareAddress) (*url.URL, error) {
	if publicURL = strings.TrimSpace(publicURL); publicURL != "" {
		u, err := url.Parse(publicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, errShareBadURL
		}
		return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}, nil
	}
	host, port, err := net.SplitHostPort(bindAddr)
	if err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(host)
	switch {
	case host == "" || (err == nil && ip.IsUnspecified()):
		addrs := private()
		if len(addrs) == 0 {
			return nil, errShareNoAddress
		}
		host = addrs[0].IP
	case err == nil && ip.IsLoopback(), strings.EqualFold(host, "localhost"):
		return nil, errShareLoopback
	}
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/"}, nil
}

// credentialMayRide reports whether a link to u may carry the token. Over
// https it is encrypted; over plain http only an address that stays off the
// internet qualifies, and a name does not, since nothing here knows where it
// resolves.
func credentialMayRide(u *url.URL) bool {
	if u.Scheme == "https" {
		return true
	}
	ip, err := netip.ParseAddr(u.Hostname())
	return err == nil && serve.OffInternet(ip)
}

// reportShareQR draws the link a phone scans to open this server, or says why
// it will not. Token mode puts the token in the link's fragment, so a link to a
// public address over plain http is refused rather than drawn.
func reportShareQR(w io.Writer, mode, token, bindAddr, publicURL string) {
	if mode != "token" && mode != "password" {
		return
	}
	base, err := shareBase(publicURL, bindAddr, serve.PrivateAddresses)
	if err != nil {
		fmt.Fprintf(w, "  phone: no QR code — %v\n", err)
		return
	}
	link := base.String()
	if mode == "token" {
		if !credentialMayRide(base) {
			fmt.Fprintf(w, "  phone: no QR code — %s is plain HTTP to an address on the internet, and the code would carry the token; put TLS in front and pass --public-url https://…\n", base.Host)
			return
		}
		link += "#token=" + url.QueryEscape(token)
	}
	code, err := qr.Encode(link, qr.L)
	if err != nil {
		fmt.Fprintf(w, "  phone: no QR code — %v\n", err)
		return
	}
	fmt.Fprintf(w, "  phone: scan to open %s\n", base.String())
	writeQR(w, code)
}

// qrQuiet is the light border a reader needs around the symbol, in modules.
const qrQuiet = 2

// writeQR draws a code two modules to a cell with half blocks, dark on an
// explicit light background, so it scans whatever the terminal's own colours.
func writeQR(w io.Writer, code *qr.Code) {
	dark := func(x, y int) bool {
		x, y = x-qrQuiet, y-qrQuiet
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	side := code.Size + 2*qrQuiet
	var b strings.Builder
	for y := 0; y < side; y += 2 {
		b.WriteString("  \x1b[30;107m")
		for x := range side {
			top, bottom := dark(x, y), dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bottom:
				b.WriteRune('▄')
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	_, _ = io.WriteString(w, b.String())
}
