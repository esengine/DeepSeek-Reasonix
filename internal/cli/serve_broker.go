package cli

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/providerbroker"
)

// brokerFlags are what a bootstrapped serve is told about the machine holding
// the model credentials. Both are set by the launch command, never by a person.
type brokerFlags struct {
	addr      *string
	file      *string
	tokenFile *string
}

func registerBrokerFlags(fs *flag.FlagSet) brokerFlags {
	return brokerFlags{
		addr: fs.String("provider-broker", "",
			"resolve providers from the machine that bootstrapped this serve, over this loopback base URL (set by `reasonix remote`)"),
		file: fs.String("provider-broker-file", "",
			"read the provider broker's loopback address and token (two lines) from this file on every request, so the broker can move without a restart (set by `reasonix remote`)"),
		tokenFile: fs.String("provider-broker-token-file", "",
			"read the provider broker's pre-shared token from this file (keeps the secret out of argv)"),
	}
}

// resolver builds the broker-backed provider.Resolver, or (nil, nil) when this
// serve was not launched with one — in which case boot keeps reading providers
// out of this machine's own config.
func (b brokerFlags) resolver() (provider.Resolver, error) {
	addr, file, tokenFile := flagValue(b.addr), flagValue(b.file), flagValue(b.tokenFile)
	if addr == "" && file == "" {
		return nil, nil
	}
	if addr != "" && file != "" {
		return nil, fmt.Errorf("--provider-broker and --provider-broker-file name the broker twice; give one")
	}
	if file == "" && tokenFile == "" {
		return nil, fmt.Errorf("--provider-broker needs --provider-broker-token-file")
	}
	endpoint := func() (string, string, error) {
		raw, token := addr, ""
		if file != "" {
			// One read of one file: the address and the token it pairs with
			// are replaced together, so they are read together.
			lines, err := readPrivateLines(file, 2)
			if err != nil {
				return "", "", fmt.Errorf("provider broker: %w", err)
			}
			raw, token = lines[0], lines[1]
		} else {
			read, err := readServeTokenFile(tokenFile)
			if err != nil {
				return "", "", fmt.Errorf("provider broker token: %w", err)
			}
			token = read
		}
		// Checked on every read: the file is rewritten while this serve runs,
		// and a broker off loopback would carry the conversation off the tunnel.
		base, err := loopbackBrokerURL(raw)
		if err != nil {
			return "", "", err
		}
		return base, token, nil
	}
	if _, _, err := endpoint(); err != nil {
		return nil, err
	}
	// No overall timeout: a completion streams as long as the model takes, and
	// the tunnel closing ends a dead one. A redirect is refused, since it would
	// carry the token and the conversation wherever the answer pointed.
	return providerbroker.NewEndpointClient(endpoint, &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			ResponseHeaderTimeout: 60 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}), nil
}

func flagValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

// loopbackBrokerURL refuses a broker that is not on this machine's loopback.
// The address arrives from the forward that published it, so a non-loopback one
// means the tunnel is not what is carrying the conversation — and the whole
// conversation, with the workspace in it, is what these requests send.
func loopbackBrokerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("--provider-broker %q: %w", raw, err)
	}
	if u.Scheme != "http" {
		return "", fmt.Errorf("--provider-broker must be http on loopback, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "localhost" {
		return strings.TrimRight(u.String(), "/"), nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("--provider-broker must be on loopback, got %q", host)
	}
	return strings.TrimRight(u.String(), "/"), nil
}
