package providerbroker

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"reasonix/internal/contract/provider"
)

// Local is this machine's broker: a Server on a loopback listener, plus the
// token a remote kernel authenticates with. An -R forward publishes Addr on
// each host that resolves providers here, so one Local serves every one of
// them — the credentials are one process's, not one connection's.
type Local struct {
	Addr  string
	Token string

	srv  *http.Server
	ln   net.Listener
	once sync.Once
}

// Listen starts a broker on loopback. Loopback is not a default but the whole
// design: the only route to it is a forward the user's own SSH connection
// carries, so a machine that was never dialled cannot reach these credentials.
func Listen(resolver provider.Resolver) (*Local, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	handler, err := NewServer(resolver, token)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	l := &Local{
		Addr:  ln.Addr().String(),
		Token: token,
		ln:    ln,
		// No WriteTimeout: a completion streams for as long as the model takes,
		// and a deadline here would cut the long turns first.
		srv: &http.Server{Handler: handler.Handler(), ReadHeaderTimeout: 10 * time.Second},
	}
	go func() {
		if serveErr := l.srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return
		}
	}()
	return l, nil
}

// Close stops the broker. Safe to call more than once.
func (l *Local) Close() error {
	if l == nil {
		return nil
	}
	var err error
	l.once.Do(func() { err = l.srv.Close() })
	return err
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
