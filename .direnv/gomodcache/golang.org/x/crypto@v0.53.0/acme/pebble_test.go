// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package acme_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/acme"
)

const (
	// pebbleModVersion is the module version used for Pebble and Pebble's
	// challenge test server. It is ignored if `-pebble-local-dir` is provided.
	pebbleModVersion = "v2.7.0"
	// startingPort is the first port number used for binding interface
	// addresses. Each call to takeNextPort() will increment a port number
	// starting at this value.
	startingPort = 5555
)

var (
	pebbleLocalDir = flag.String(
		"pebble-local-dir",
		"",
		"Local Pebble to use, instead of fetching from source",
	)
	nextPort atomic.Uint32
)

func init() {
	nextPort.Store(startingPort)
}

func TestWithPebble(t *testing.T) {
	// We want to use process groups w/ syscall.Kill, and the acme package
	// is very platform-agnostic, so skip on non-Linux.
	if runtime.GOOS != "linux" {
		t.Skip("skipping pebble tests on non-linux OS")
	}

	if testing.Short() {
		t.Skip("skipping pebble tests in short mode")
	}

	tests := []struct {
		name     string
		challSrv func(*environment) (challengeServer, string)
	}{
		{
			name: "TLSALPN01-Issuance",
			challSrv: func(env *environment) (challengeServer, string) {
				bindAddr := fmt.Sprintf(":%d", env.config.TLSPort)
				return newChallTLSServer(bindAddr), bindAddr
			},
		},

		{
			name: "HTTP01-Issuance",
			challSrv: func(env *environment) (challengeServer, string) {
				bindAddr := fmt.Sprintf(":%d", env.config.HTTPPort)
				return newChallHTTPServer(bindAddr), bindAddr
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := startPebbleEnvironment(t, nil)
			challSrv, challSrvAddr := tt.challSrv(&env)
			challSrv.Run()

			t.Cleanup(func() {
				challSrv.Shutdown()
			})

			waitForServer(t, challSrvAddr)
			testIssuance(t, &env, challSrv)
		})
	}
}

// challengeServer abstracts over the details of running a challenge response
// server for some supported acme.Challenge type. Responses are provisioned
// during the test issuance process to be presented to the ACME server's
// validation authority.
type challengeServer interface {
	Run()
	Shutdown() error
	Supported(chal *acme.Challenge) bool
	Provision(client *acme.Client, ident acme.AuthzID, chal *acme.Challenge) error
}

// challTLSServer is a simple challenge response server that listens for TLS
// connections on a specific port and if they are TLS-ALPN-01 challenge
// requests, completes the handshake using the configured challenge response
// certificate for the SNI value provided.
type challTLSServer struct {
	*http.Server
	// mu protects challCerts.
	mu sync.RWMutex
	// challCerts is a map from SNI domain name to challenge response certificate.
	challCerts map[string]*tls.Certificate
}

// https://datatracker.ietf.org/doc/html/rfc8737#section-4
const acmeTLSAlpnProtocol = "acme-tls/1"

func newChallTLSServer(address string) *challTLSServer {
	challServer := &challTLSServer{Server: &http.Server{
		Addr:         address,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}, challCerts: make(map[string]*tls.Certificate)}

	// Configure the server to support the TLS-ALPN-01 challenge protocol
	// and to use a callback for selecting the handshake certificate.
	challServer.Server.TLSConfig = &tls.Config{
		NextProtos:     []string{acmeTLSAlpnProtocol},
		GetCertificate: challServer.getCertificate,
	}

	return challServer
}

func (c *challTLSServer) Shutdown() error {
	log.Printf("challTLSServer: shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10)
	defer cancel()
	return c.Server.Shutdown(ctx)
}

func (c *challTLSServer) Run() {
	go func() {
		// Note: certFile and keyFile are empty because our config uses a
		// GetCertificate callback.
		if err := c.Server.ListenAndServeTLS("", ""); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				log.Printf("challTLSServer error: %v", err)
			}
		}
	}()
}

func (c *challTLSServer) Supported(chal *acme.Challenge) bool {
	return chal.Type == "tls-alpn-01"
}

func (c *challTLSServer) Provision(client *acme.Client, ident acme.AuthzID, chal *acme.Challenge) error {
	respCert, err := client.TLSALPN01ChallengeCert(chal.Token, ident.Value)
	if err != nil {
		return fmt.Errorf("challTLSServer: failed to generate challlenge response cert for %s: %w",
			ident.Value, err)
	}

	log.Printf("challTLSServer: setting challenge response certificate for %s", ident.Value)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.challCerts[ident.Value] = &respCert

	return nil
}

func (c *challTLSServer) getCertificate(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	// Verify the request looks like a TLS-ALPN-01 challenge request.
	if len(clientHello.SupportedProtos) != 1 || clientHello.SupportedProtos[0] != acmeTLSAlpnProtocol {
		return nil, fmt.Errorf(
			"challTLSServer: non-TLS-ALPN-01 challenge request received with SupportedProtos: %s",
			clientHello.SupportedProtos)
	}

	serverName := clientHello.ServerName

	// TLS-ALPN-01 challenge requests for IP addresses are encoded in the SNI
	// using the reverse-DNS notation. See RFC 8738 Section 6:
	//   https://www.rfc-editor.org/rfc/rfc8738.html#section-6
	if strings.HasSuffix(serverName, ".in-addr.arpa") {
		serverName = strings.TrimSuffix(serverName, ".in-addr.arpa")
		parts := strings.Split(serverName, ".")
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		serverName = strings.Join(parts, ".")
	}

	log.Printf("challTLSServer: selecting certificate for request from %s for %s",
		clientHello.Conn.RemoteAddr(), serverName)

	c.mu.RLock()
	defer c.mu.RUnlock()
	cert := c.challCerts[serverName]
	if cert == nil {
		return nil, fmt.Errorf("challTLSServer: no challenge response certificate configured for %s", serverName)
	}

	return cert, nil
}

// challHTTPServer is a simple challenge response server that listens for HTTP
// connections on a specific port and if they are HTTP-01 challenge requests,
// serves the challenge response key authorization.
type challHTTPServer struct {
	*http.Server
	// mu protects challMap
	mu sync.RWMutex
	// challMap is a mapping from request path to response body.
	challMap map[string]string
}

func newChallHTTPServer(address string) *challHTTPServer {
	challServer := &challHTTPServer{
		Server: &http.Server{
			Addr:         address,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
		},
		challMap: make(map[string]string),
	}

	challServer.Server.Handler = challServer

	return challServer
}

func (c *challHTTPServer) Supported(chal *acme.Challenge) bool {
	return chal.Type == "http-01"
}

func (c *challHTTPServer) Provision(client *acme.Client, ident acme.AuthzID, chall *acme.Challenge) error {
	path := client.HTTP01ChallengePath(chall.Token)
	body, err := client.HTTP01ChallengeResponse(chall.Token)
	if err != nil {
		return fmt.Errorf("failed to generate HTTP-01 challenge response for %v challenge %s token %s: %w",
			ident, chall.URI, chall.Token, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	log.Printf("challHTTPServer: setting challenge response for %s", path)
	c.challMap[path] = body

	return nil
}

func (c *challHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("challHTTPServer: handling %s to %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	response, exists := c.challMap[r.URL.Path]

	if !exists {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(response))
}

func (c *challHTTPServer) Shutdown() error {
	log.Printf("challHTTPServer: shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.Server.Shutdown(ctx)
}

func (c *challHTTPServer) Run() {
	go func() {
		if err := c.Server.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				log.Printf("challHTTPServer error: %v", err)
			}
		}
	}()
}

func testIssuance(t *testing.T, env *environment, challSrv challengeServer) {
	t.Helper()

	// Bound the total issuance process by a timeout of 60 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a new ACME account.
	client := env.client
	acct, err := client.Register(ctx, &acme.Account{}, acme.AcceptTOS)
	if err != nil {
		t.Fatalf("failed to register account: %v", err)
	}
	if acct.Status != acme.StatusValid {
		t.Fatalf("expected new account status to be valid, got %v", acct.Status)
	}
	log.Printf("registered account: %s", acct.URI)

	// Create a new order for some example identifiers
	identifiers := []acme.AuthzID{
		{
			Type:  "dns",
			Value: "example.com",
		},
		{
			Type:  "dns",
			Value: "www.example.com",
		},
		{
			Type:  "ip",
			Value: "127.0.0.1",
		},
	}
	order, err := client.AuthorizeOrder(ctx, identifiers)
	if err != nil {
		t.Fatalf("failed to create order for %v: %v", identifiers, err)
	}
	if order.Status != acme.StatusPending {
		t.Fatalf("expected new order status to be pending, got %v", order.Status)
	}
	orderURL := order.URI
	log.Printf("created order: %v", orderURL)

	// For each pending authz provision a supported challenge type's response
	// with the test challenge server, and tell the ACME server to verify it.
	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			t.Fatalf("failed to get order %s authorization %s: %v",
				orderURL, authzURL, err)
		}

		if authz.Status != acme.StatusPending {
			continue
		}

		for _, challenge := range authz.Challenges {
			if challenge.Status != acme.StatusPending || !challSrv.Supported(challenge) {
				continue
			}

			if err := challSrv.Provision(client, authz.Identifier, challenge); err != nil {
				t.Fatalf("failed to provision challenge %s: %v", challenge.URI, err)
			}

			_, err = client.Accept(ctx, challenge)
			if err != nil {
				t.Fatalf("failed to accept order %s challenge %s: %v",
					orderURL, challenge.URI, err)
			}
		}
	}

	// Wait for the order to become ready for finalization.
	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		var orderErr *acme.OrderError
		if errors.Is(err, orderErr) {
			t.Fatalf("failed to wait for order %s: %s: %s", orderURL, err, orderErr.Problem)
		} else {
			t.Fatalf("failed to wait for order %s: %s", orderURL, err)
		}
	}
	if order.Status != acme.StatusReady {
		t.Fatalf("expected order %s status to be ready, got %v",
			orderURL, order.Status)
	}

	// Generate a certificate keypair and a CSR for the order identifiers.
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate certificate key: %v", err)
	}
	var dnsNames []string
	var ipAddresses []net.IP
	for _, ident := range identifiers {
		switch ident.Type {
		case "dns":
			dnsNames = append(dnsNames, ident.Value)
		case "ip":
			ipAddresses = append(ipAddresses, net.ParseIP(ident.Value))
		default:
			t.Fatalf("unsupported identifier type: %s", ident.Type)
		}
	}
	csrDer, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		DNSNames:    dnsNames,
		IPAddresses: ipAddresses,
	}, certKey)
	if err != nil {
		t.Fatalf("failed to create CSR: %v", err)
	}

	// Finalize the order by creating a certificate with our CSR.
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csrDer, true)
	if err != nil {
		t.Fatalf("failed to finalize order %s with finalize URL %s: %v",
			orderURL, order.FinalizeURL, err)
	}

	// Split the chain into the leaf and any intermediates.
	leaf := chain[0]
	intermediatesDER := chain[1:]
	leafCert, err := x509.ParseCertificate(leaf)
	if err != nil {
		t.Fatalf("failed to parse order %s leaf certificate: %v", orderURL, err)
	}
	intermediates := x509.NewCertPool()
	for i, intermediateDER := range intermediatesDER {
		intermediate, err := x509.ParseCertificate(intermediateDER)
		if err != nil {
			t.Fatalf("failed to parse intermediate %d: %v", i, err)
		}
		intermediates.AddCert(intermediate)
	}

	// Verify there is a valid path from the leaf certificate to Pebble's
	// issuing root using the provided intermediate certificates.
	roots, err := env.RootCert()
	if err != nil {
		t.Fatalf("failed to get Pebble issuer root certs: %v", err)
	}
	paths, err := leafCert.Verify(x509.VerifyOptions{
		Intermediates: intermediates,
		Roots:         roots,
	})
	if err != nil {
		t.Fatalf("failed to verify order %s leaf certificate: %v", orderURL, err)
	}
	log.Printf("verified %d path(s) from issued leaf certificate to Pebble root CA", len(paths))

	// Also verify that the leaf cert is valid for each of the DNS names
	// and IP addresses from our order's identifiers.
	for _, name := range dnsNames {
		if err := leafCert.VerifyHostname(name); err != nil {
			t.Fatalf("failed to verify order %s leaf certificate for order DNS name %s: %v",
				orderURL, name, err)
		}
	}
	for _, ip := range ipAddresses {
		if err := leafCert.VerifyHostname(ip.String()); err != nil {
			t.Fatalf("failed to verify order %s leaf certificate for order IP address %s: %v",
				orderURL, ip, err)
		}
	}
}

type environment struct {
	config *environmentConfig
	client *acme.Client
}

// RootCert returns the Pebble CA's primary issuing hierarchy root certificate.
// This is generated randomly at each startup and can be used to verify
// certificate chains issued by Pebble's ACME interface. Note that this
// is separate from the static root certificate used by the Pebble ACME
// HTTPS interface.
func (e *environment) RootCert() (*x509.CertPool, error) {
	// NOTE: in the future we may want to consider the alternative chains
	// 		 returned as Link alternative headers.
	rootURL := fmt.Sprintf("https://%s/roots/0", e.config.pebbleConfig.ManagementListenAddress)
	resp, err := e.client.HTTPClient.Get(rootURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to GET Pebble root CA from %s: %v", rootURL, err)
	}

	roots := x509.NewCertPool()
	rootPEM, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Pebble root CA PEM: %v", err)
	}
	rootDERBlock, _ := pem.Decode(rootPEM)
	rootCA, err := x509.ParseCertificate(rootDERBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Pebble root CA DER: %v", err)
	}
	roots.AddCert(rootCA)

	return roots, nil
}

// environmentConfig describes the Pebble configuration, and configuration
// shared between pebble and pebble-challtestsrv.
type environmentConfig struct {
	pebbleConfig
	dnsPort uint32
}

// defaultConfig returns an environmentConfig populated with default values.
// The provided pebbleDir is used to specify certificate/private key paths
// for the HTTPS ACME interface.
func defaultConfig(pebbleDir string) environmentConfig {
	return environmentConfig{
		pebbleConfig: pebbleConfig{
			ListenAddress:                  fmt.Sprintf("127.0.0.1:%d", takeNextPort()),
			ManagementListenAddress:        fmt.Sprintf("127.0.0.1:%d", takeNextPort()),
			HTTPPort:                       takeNextPort(),
			TLSPort:                        takeNextPort(),
			Certificate:                    fmt.Sprintf("%s/test/certs/localhost/cert.pem", pebbleDir),
			PrivateKey:                     fmt.Sprintf("%s/test/certs/localhost/key.pem", pebbleDir),
			OCSPResponderURL:               "",
			ExternalAccountBindingRequired: false,
			ExternalAccountMACKeys:         make(map[string]string),
			DomainBlocklist:                []string{"blocked-domain.example"},
			Profiles: map[string]struct {
				Description    string
				ValidityPeriod uint64
			}{
				"default": {
					Description:    "default profile",
					ValidityPeriod: 3600,
				},
			},
			RetryAfter: struct {
				Authz int
				Order int
			}{
				3,
				5,
			},
		},
		dnsPort: takeNextPort(),
	}
}

// pebbleConfig matches the JSON structure of the Pebble configuration file.
type pebbleConfig struct {
	ListenAddress                  string
	ManagementListenAddress        string
	HTTPPort                       uint32
	TLSPort                        uint32
	Certificate                    string
	PrivateKey                     string
	OCSPResponderURL               string
	ExternalAccountBindingRequired bool
	ExternalAccountMACKeys         map[string]string
	DomainBlocklist                []string
	Profiles                       map[string]struct {
		Description    string
		ValidityPeriod uint64
	}
	RetryAfter struct {
		Authz int
		Order int
	}
}

func takeNextPort() uint32 {
	return nextPort.Add(1) - 1
}

// startPebbleEnvironment is a test helper that spawns Pebble and Pebble
// challenge test server processes based on the provided environmentConfig. The
// processes will be torn down when the test ends.
func startPebbleEnvironment(t *testing.T, config *environmentConfig) environment {
	t.Helper()

	var pebbleDir string
	if *pebbleLocalDir != "" {
		pebbleDir = *pebbleLocalDir
	} else {
		pebbleDir = fetchModule(t, "github.com/letsencrypt/pebble/v2", pebbleModVersion)
	}

	binDir := prepareBinaries(t, pebbleDir)

	if config == nil {
		cfg := defaultConfig(pebbleDir)
		config = &cfg
	}

	marshalConfig := struct {
		Pebble pebbleConfig
	}{
		Pebble: config.pebbleConfig,
	}

	configData, err := json.Marshal(marshalConfig)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	configFile, err := os.CreateTemp("", "pebble-config-*.json")
	if err != nil {
		t.Fatalf("failed to create temp config file: %v", err)
	}
	t.Cleanup(func() { os.Remove(configFile.Name()) })

	if _, err := configFile.Write(configData); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	configFile.Close()

	log.Printf("pebble dir: %s", pebbleDir)
	log.Printf("config file: %s", configFile.Name())

	// Spawn the Pebble CA server. It answers ACME requests and performs
	// outbound validations. We configure it to use a mock DNS server that
	// always answers 127.0.0.1 for all A queries so that validation
	// requests for any domain name will resolve to our local challenge
	// server instances.
	spawnServerProcess(t, binDir, "pebble", "-config", configFile.Name(),
		"-dnsserver", fmt.Sprintf("127.0.0.1:%d", config.dnsPort),
		"-strict")

	// Spawn the Pebble challenge test server. We'll use it to mock DNS
	// responses but disable all the other interfaces. We want to stand
	// up our own challenge response servers for TLS-ALPN-01,
	// etc.
	// Note: we specify -defaultIPv6 "" so that no AAAA records are served.
	// The LUCI CI runners have issues with IPv6 connectivity on localhost.
	spawnServerProcess(t, binDir, "pebble-challtestsrv",
		"-dns01", fmt.Sprintf(":%d", config.dnsPort),
		"-defaultIPv6", "",
		"-management", fmt.Sprintf(":%d", takeNextPort()),
		"-doh", "",
		"-http01", "",
		"-tlsalpn01", "",
		"-https01", "")

	waitForServer(t, config.pebbleConfig.ListenAddress)
	waitForServer(t, fmt.Sprintf("127.0.0.1:%d", config.dnsPort))

	log.Printf("pebble environment ready")

	// Construct a cert pool that contains the CA certificate used by the ACME
	// interface's certificate chain. This is separate from the issuing
	// hierarchy and is used for the ACME client to interact with the ACME
	// interface without cert verification error.
	caCertPath := filepath.Join(pebbleDir, "test/certs/pebble.minica.pem")
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatalf("failed to read CA certificate %s: %v", caCertPath, err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		t.Fatalf("failed to parse CA certificate %s", caCertPath)
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
	}

	// Create an ACME account keypair/client and verify it can discover
	// the Pebble server's ACME directory without error.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate account key: %v", err)
	}
	client := &acme.Client{
		Key:          key,
		HTTPClient:   httpClient,
		DirectoryURL: fmt.Sprintf("https://%s/dir", config.ListenAddress),
	}
	_, err = client.Discover(context.TODO())
	if err != nil {
		t.Fatalf("failed to discover ACME directory: %v", err)
	}

	return environment{
		config: config,
		client: client,
	}
}

func waitForServer(t *testing.T, addr string) {
	t.Helper()

	for i := 0; i < 20; i++ {
		if conn, err := net.Dial("tcp", addr); err == nil {
			conn.Close()
			return
		}
		time.Sleep(time.Duration(i*100) * time.Millisecond)
	}
	t.Fatalf("failed to connect to %q after 20 tries", addr)
}

// fetchModule fetches the module at the given version and returns the directory
// containing its source tree. It skips the test if fetching modules is not
// possible in this environment.
//
// Copied from the stdlib cryptotest.FetchModule and adapted to not rely on the
// stdlib internal testenv package.
func fetchModule(t *testing.T, module, version string) string {
	// If the default GOMODCACHE doesn't exist, use a temporary directory
	// instead. (For example, run.bash sets GOPATH=/nonexist-gopath.)
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Errorf("go env GOMODCACHE: %v\n%s", err, out)
		if ee, ok := err.(*exec.ExitError); ok {
			t.Logf("%s", ee.Stderr)
		}
		t.FailNow()
	}
	modcacheOk := false
	if gomodcache := string(bytes.TrimSpace(out)); gomodcache != "" {
		if _, err := os.Stat(gomodcache); err == nil {
			modcacheOk = true
		}
	}
	if !modcacheOk {
		t.Setenv("GOMODCACHE", t.TempDir())
		// Allow t.TempDir() to clean up subdirectories.
		t.Setenv("GOFLAGS", os.Getenv("GOFLAGS")+" -modcacherw")
	}

	t.Logf("fetching %s@%s\n", module, version)

	output, err := exec.Command("go", "mod", "download", "-json", module+"@"+version).CombinedOutput()
	if err != nil {
		t.Fatalf("failed to download %s@%s: %s\n%s\n", module, version, err, output)
	}
	var j struct {
		Dir string
	}
	if err := json.Unmarshal(output, &j); err != nil {
		t.Fatalf("failed to parse 'go mod download': %s\n%s\n", err, output)
	}

	return j.Dir
}

func prepareBinaries(t *testing.T, pebbleDir string) string {
	t.Helper()

	// We don't want to build in the module cache dir, which might not be
	// writable or to pollute the user's clone with binaries if pebbleLocalDir
	// is used.
	binDir := t.TempDir()

	build := func(cmd string) {
		log.Printf("building %s", cmd)
		buildCmd := exec.Command(
			"go",
			"build", "-o", filepath.Join(binDir, cmd), "-mod", "mod", "./cmd/"+cmd)
		buildCmd.Dir = pebbleDir
		output, err := buildCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to build %s: %s\n%s\n", cmd, err, output)
		}
	}

	build("pebble")
	build("pebble-challtestsrv")

	return binDir
}

func spawnServerProcess(t *testing.T, dir string, cmd string, args ...string) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmdInstance := exec.Command("./"+cmd, args...)
	cmdInstance.Dir = dir
	cmdInstance.Stdout = &stdout
	cmdInstance.Stderr = &stderr

	if err := cmdInstance.Start(); err != nil {
		t.Fatalf("failed to start %s: %v", cmd, err)
	}

	t.Cleanup(func() {
		cmdInstance.Process.Kill()
		cmdInstance.Wait()

		if t.Failed() || testing.Verbose() {
			t.Logf("=== %s output ===", cmd)
			if stdout.Len() > 0 {
				t.Logf("stdout:\n%s", strings.TrimSpace(stdout.String()))
			}
			if stderr.Len() > 0 {
				t.Logf("stderr:\n%s", strings.TrimSpace(stderr.String()))
			}
		}
	})
}
