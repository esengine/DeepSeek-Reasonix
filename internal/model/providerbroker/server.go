package providerbroker

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"reasonix/internal/contract/provider"
)

// ErrNoToken refuses a Server built without one. An unauthenticated broker on
// the remote's loopback is a model-spend capability for every account on that
// machine, so the gate is not optional.
var ErrNoToken = errors.New("providerbroker: a token is required")

// Server answers a remote kernel's catalog and stream calls out of a resolver
// holding this machine's credentials. Nothing it returns carries a key: the
// far side learns which providers exist and what they answered, never how to
// reach one.
type Server struct {
	resolver provider.Resolver
	token    string
}

func NewServer(resolver provider.Resolver, token string) (*Server, error) {
	if resolver == nil {
		return nil, errors.New("providerbroker: a resolver is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, ErrNoToken
	}
	return &Server{resolver: resolver, token: token}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(PathCatalog, s.guard(s.handleCatalog))
	mux.HandleFunc(PathStream, s.guard(s.handleStream))
	return mux
}

// guard authenticates before routing. A wrong token is answered the same way
// a missing one is: which of the two it was is the caller's own business.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(r.Header.Get(HeaderToken))
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeWireError(w, http.StatusUnauthorized, errors.New("providerbroker: token rejected"))
			return
		}
		next(w, r)
	}
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWireError(w, http.StatusMethodNotAllowed, errors.New("providerbroker: catalog is GET"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(catalogResponse{Providers: s.resolver.Catalog()})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWireError(w, http.StatusMethodNotAllowed, errors.New("providerbroker: stream is POST"))
		return
	}
	var req streamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeWireError(w, http.StatusBadRequest, err)
		return
	}
	prov, err := s.resolver.Resolve(req.Selection)
	if err != nil {
		we := encodeError(err)
		we.Ref = strings.TrimSpace(req.Selection.Ref)
		writeWire(w, http.StatusNotFound, we)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	chunks, err := prov.Stream(ctx, req.Request)
	if err != nil {
		writeWireError(w, http.StatusBadGateway, err)
		return
	}
	// Past this point the status is spent, so a failure can only travel as a
	// chunk. The provider's own contract says the same thing.
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	enc := json.NewEncoder(w)
	broken := false
	for c := range chunks {
		if broken {
			continue // drain: the provider closes its channel only when read out
		}
		if err := enc.Encode(encodeChunk(c)); err != nil {
			broken = true
			cancel()
			continue
		}
		if err := rc.Flush(); err != nil {
			broken = true
			cancel()
		}
	}
}

// writeWireError answers a request that never became a stream. The status
// separates "it did not start" from "it started and then failed"; the body
// carries the identity, so a 502 here still arrives as *provider.APIError.
func writeWireError(w http.ResponseWriter, status int, err error) {
	writeWire(w, status, encodeError(err))
}

func writeWire(w http.ResponseWriter, status int, we *wireError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(we)
}
