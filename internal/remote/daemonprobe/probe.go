// Package daemonprobe reads the running Remote daemon Build ID over the
// current user's trusted service endpoint. It deliberately uses only the
// frozen initialize/detach protocol: V1 has no hidden status RPC and the probe
// never asks systemd to change service state.
package daemonprobe

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"reasonix/internal/nilutil"
	"reasonix/internal/remote/lifecycle"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
	"reasonix/internal/rpcwire"
)

const (
	defaultAttemptTimeout = 2 * time.Second
	defaultMaxSeries      = 8
	maximumCandidateTries = 16
)

var (
	// ErrDaemonUnstable means no two consecutive, complete Build ID reads
	// agreed. Callers should report this state; a read-only status operation
	// must not restart or otherwise repair the daemon.
	ErrDaemonUnstable = errors.New("Remote daemon changed repeatedly while its Build ID was being read")
	errSeriesChanged  = errors.New("Remote daemon changed during Build ID recovery")
)

type endpointDialer interface {
	Dial(context.Context) (net.Conn, error)
}

type candidateFactory func() (protocol.BuildID, error)
type clientIDFactory func() (protocol.ClientInstanceID, error)

// Client implements lifecycle.DaemonProbe against a secure service.Endpoint.
// New accepts the concrete endpoint intentionally: production callers cannot
// substitute a TCP transport and bypass the endpoint's owner/socket checks.
type Client struct {
	dialer         endpointDialer
	attemptTimeout time.Duration
	maxSeries      int
	newCandidate   candidateFactory
	newClientID    clientIDFactory
}

var _ lifecycle.DaemonProbe = (*Client)(nil)

// New constructs a read-only daemon identity probe.
func New(endpoint *service.Endpoint) (*Client, error) {
	if endpoint == nil {
		return nil, errors.New("Remote daemon probe endpoint is required")
	}
	return newClient(endpoint), nil
}

func newClient(dialer endpointDialer) *Client {
	return &Client{
		dialer:         dialer,
		attemptTimeout: defaultAttemptTimeout,
		maxSeries:      defaultMaxSeries,
		newCandidate:   randomCandidate,
		newClientID:    randomClientID,
	}
}

// Probe recovers the daemon-owned expected value for each Build ID field from
// strict DAEMON_RESTART_REQUIRED initialize responses. CompareBuildID checks
// fields in frozen order with expected=daemon and actual=probe candidate.
//
// Two consecutive recovery series must agree. This catches a daemon restart
// between connections without acquiring a lease in the normal path. In the
// extremely unlikely case that every unknown sentinel already equals the
// daemon, initialize succeeds; Probe immediately sends remote/detach and does
// not retain the temporary lease.
func (c *Client) Probe(ctx context.Context) (lifecycle.DaemonProbeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return lifecycle.DaemonProbeResult{}, err
	}
	if c == nil || nilutil.IsNil(c.dialer) {
		return lifecycle.DaemonProbeResult{}, errors.New("Remote daemon probe endpoint is required")
	}
	if c.attemptTimeout <= 0 {
		return lifecycle.DaemonProbeResult{}, errors.New("Remote daemon probe attempt timeout must be positive")
	}
	if c.maxSeries < 2 {
		return lifecycle.DaemonProbeResult{}, errors.New("Remote daemon probe requires at least two identity series")
	}
	if c.newCandidate == nil || c.newClientID == nil {
		return lifecycle.DaemonProbeResult{}, errors.New("Remote daemon probe identity generators are required")
	}

	var previous *protocol.BuildID
	for series := 0; series < c.maxSeries; series++ {
		if err := ctx.Err(); err != nil {
			return lifecycle.DaemonProbeResult{}, err
		}
		buildID, err := c.recoverSeries(ctx)
		if errors.Is(err, errSeriesChanged) {
			previous = nil
			continue
		}
		if err != nil {
			return lifecycle.DaemonProbeResult{}, err
		}
		if previous != nil && protocol.CompareBuildID(*previous, buildID) == nil {
			return lifecycle.DaemonProbeResult{BuildID: buildID}, nil
		}
		copyValue := buildID
		previous = &copyValue
	}
	return lifecycle.DaemonProbeResult{}, ErrDaemonUnstable
}

type buildField uint8

const (
	fieldProduct buildField = iota
	fieldRevision
	fieldProtocol
	fieldSchema
	fieldCount
)

func (c *Client) recoverSeries(ctx context.Context) (protocol.BuildID, error) {
	var known [fieldCount]bool
	var values [fieldCount]string

	// Every mismatch learns at least one new field. The extra request is only
	// reachable when all remaining sentinels exactly match and initialize
	// succeeds (or reports HOST_BUSY after the successful Build ID comparison).
	for request := 0; request <= int(fieldCount); request++ {
		candidate, err := c.nextCandidate(known, values)
		if err != nil {
			return protocol.BuildID{}, err
		}
		outcome, err := c.initializeOnce(ctx, candidate)
		if err != nil {
			return protocol.BuildID{}, err
		}
		switch outcome.kind {
		case initializeExact, initializeBusy:
			return outcome.buildID, nil
		case initializeMismatch:
			changed, err := learnMismatch(&known, &values, candidate, outcome.mismatch)
			if err != nil {
				return protocol.BuildID{}, err
			}
			if changed {
				return protocol.BuildID{}, errSeriesChanged
			}
			if allFieldsKnown(known) {
				buildID := buildIDFromValues(values)
				if err := buildID.Validate(); err != nil {
					return protocol.BuildID{}, fmt.Errorf("Remote daemon returned an invalid Build ID: %w", err)
				}
				return buildID, nil
			}
		default:
			return protocol.BuildID{}, errors.New("Remote daemon probe reached an invalid initialize state")
		}
	}
	return protocol.BuildID{}, errors.New("Remote daemon did not yield a complete Build ID")
}

func (c *Client) nextCandidate(known [fieldCount]bool, values [fieldCount]string) (protocol.BuildID, error) {
	for attempt := 0; attempt < maximumCandidateTries; attempt++ {
		candidate, err := c.newCandidate()
		if err != nil {
			return protocol.BuildID{}, fmt.Errorf("generate Remote daemon probe candidate: %w", err)
		}
		candidateValues := valuesFromBuildID(candidate)
		for field := buildField(0); field < fieldCount; field++ {
			if known[field] {
				candidateValues[field] = values[field]
			}
		}
		if !unknownValuesUnique(known, candidateValues) {
			continue
		}
		candidate = buildIDFromValues(candidateValues)
		if err := candidate.Validate(); err != nil {
			return protocol.BuildID{}, fmt.Errorf("generated Remote daemon probe candidate is invalid: %w", err)
		}
		return candidate, nil
	}
	return protocol.BuildID{}, errors.New("Remote daemon probe candidate generator repeatedly returned ambiguous field values")
}

func unknownValuesUnique(known [fieldCount]bool, values [fieldCount]string) bool {
	for left := buildField(0); left < fieldCount; left++ {
		if known[left] {
			continue
		}
		for right := buildField(0); right < fieldCount; right++ {
			if left != right && values[left] == values[right] {
				return false
			}
		}
	}
	return true
}

func learnMismatch(
	known *[fieldCount]bool,
	values *[fieldCount]string,
	candidate protocol.BuildID,
	data protocol.RemoteErrorData,
) (bool, error) {
	candidateValues := valuesFromBuildID(candidate)
	for field := buildField(0); field < fieldCount; field++ {
		if known[field] && candidateValues[field] == data.Actual {
			// A field already recovered from this series no longer matches. The
			// only compliant explanation is that the daemon changed between the
			// two fresh Unix-socket connections.
			return true, nil
		}
	}

	mismatchField := fieldCount
	for field := buildField(0); field < fieldCount; field++ {
		if !known[field] && candidateValues[field] == data.Actual {
			if mismatchField != fieldCount {
				return false, errors.New("Remote daemon returned an ambiguous Build ID mismatch")
			}
			mismatchField = field
		}
	}
	if mismatchField == fieldCount {
		return false, errors.New("Remote daemon Build ID mismatch does not identify a probe candidate field")
	}
	if data.Expected == data.Actual {
		return false, errors.New("Remote daemon returned equal expected and actual Build ID mismatch values")
	}
	if err := validateBuildField(mismatchField, data.Expected); err != nil {
		return false, fmt.Errorf("Remote daemon returned an invalid expected Build ID field: %w", err)
	}

	// CompareBuildID can reach mismatchField only when every preceding field
	// equals the daemon. Any still-unknown prefix candidate is therefore also a
	// recovered daemon value.
	for field := buildField(0); field < mismatchField; field++ {
		if !known[field] {
			known[field] = true
			values[field] = candidateValues[field]
		}
	}
	known[mismatchField] = true
	values[mismatchField] = data.Expected
	return false, nil
}

func validateBuildField(field buildField, value string) error {
	base := protocol.BuildID{
		ProductVersion:  "probe",
		SourceRevision:  strings.Repeat("0", 40),
		ProtocolVersion: "probe",
		SchemaHash:      "sha256:" + strings.Repeat("0", 64),
	}
	values := valuesFromBuildID(base)
	values[field] = value
	return buildIDFromValues(values).Validate()
}

func allFieldsKnown(known [fieldCount]bool) bool {
	for field := buildField(0); field < fieldCount; field++ {
		if !known[field] {
			return false
		}
	}
	return true
}

func valuesFromBuildID(buildID protocol.BuildID) [fieldCount]string {
	return [fieldCount]string{
		buildID.ProductVersion,
		buildID.SourceRevision,
		buildID.ProtocolVersion,
		buildID.SchemaHash,
	}
}

func buildIDFromValues(values [fieldCount]string) protocol.BuildID {
	return protocol.BuildID{
		ProductVersion:  values[fieldProduct],
		SourceRevision:  values[fieldRevision],
		ProtocolVersion: values[fieldProtocol],
		SchemaHash:      values[fieldSchema],
	}
}

type initializeKind uint8

const (
	initializeMismatch initializeKind = iota
	initializeBusy
	initializeExact
)

type initializeOutcome struct {
	kind     initializeKind
	buildID  protocol.BuildID
	mismatch protocol.RemoteErrorData
}

func (c *Client) initializeOnce(ctx context.Context, candidate protocol.BuildID) (initializeOutcome, error) {
	attemptCtx, cancelAttempt := context.WithTimeout(ctx, c.attemptTimeout)
	defer cancelAttempt()

	raw, err := c.dialer.Dial(attemptCtx)
	if err != nil {
		if !nilutil.IsNil(raw) {
			_ = raw.Close()
		}
		if contextErr := attemptCtx.Err(); contextErr != nil {
			return initializeOutcome{}, contextErr
		}
		return initializeOutcome{}, fmt.Errorf("dial trusted Remote daemon endpoint: %w", err)
	}
	if nilutil.IsNil(raw) {
		return initializeOutcome{}, errors.New("Remote daemon endpoint returned a nil connection")
	}

	serveCtx, cancelServe := context.WithCancel(context.Background())
	wire := rpcwire.NewConn(raw, raw, rpcwire.Options{
		Name:             "remote-daemon-build-probe",
		MaxInboundBytes:  protocol.FrameBytes,
		MaxOutboundBytes: protocol.FrameBytes,
		StrictJSONRPC:    true,
	})
	serveDone := make(chan error, 1)
	go func() { serveDone <- wire.Serve(serveCtx) }()
	defer func() {
		cancelServe()
		_ = raw.Close()
		<-serveDone
	}()

	clientID, err := c.newClientID()
	if err != nil {
		return initializeOutcome{}, fmt.Errorf("generate Remote daemon probe client identity: %w", err)
	}
	if strings.TrimSpace(string(clientID)) == "" {
		return initializeOutcome{}, errors.New("generated Remote daemon probe client identity is empty")
	}
	rawResult, requestErr := wire.Request(attemptCtx, string(protocol.MethodRemoteInitialize), protocol.InitializeParams{
		BuildID: candidate, ClientInstanceID: clientID,
	})
	if requestErr != nil {
		var response *rpcwire.ResponseError
		if !errors.As(requestErr, &response) {
			if contextErr := attemptCtx.Err(); contextErr != nil {
				return initializeOutcome{}, contextErr
			}
			return initializeOutcome{}, fmt.Errorf("read Remote daemon initialize response: %w", requestErr)
		}
		data, err := decodeRemoteError(response)
		if err != nil {
			return initializeOutcome{}, err
		}
		switch data.ReasonixCode {
		case protocol.ErrDaemonRestartRequired:
			if data.Expected == "" || data.Actual == "" {
				return initializeOutcome{}, errors.New("Remote daemon Build ID mismatch omitted expected or actual")
			}
			return initializeOutcome{kind: initializeMismatch, mismatch: data}, nil
		case protocol.ErrHostBusy:
			if data.Expected != "" || data.Actual != "" {
				return initializeOutcome{}, errors.New("Remote daemon HOST_BUSY response carried Build ID mismatch fields")
			}
			// Build comparison precedes lease acquisition. HOST_BUSY therefore
			// proves the complete candidate is the running daemon Build ID, and
			// this probe did not acquire a lease.
			return initializeOutcome{kind: initializeBusy, buildID: candidate}, nil
		default:
			return initializeOutcome{}, fmt.Errorf("Remote daemon initialize returned unexpected structured error %s", data.ReasonixCode)
		}
	}

	var result protocol.InitializeResult
	if err := decodeStrict(rawResult, &result); err != nil {
		return initializeOutcome{}, fmt.Errorf("decode Remote daemon initialize result: %w", err)
	}
	if err := validateInitializeResult(candidate, result); err != nil {
		return initializeOutcome{}, err
	}

	// Exact sentinel collision is possible in principle. Release the temporary
	// lease through the protocol's response-before-release path before closing
	// the transport; never rely on connection EOF, which intentionally retains
	// a normal Desktop lease for reconnection.
	rawDetach, err := wire.Request(attemptCtx, string(protocol.MethodRemoteDetach), protocol.DetachParams{LeaseID: result.Lease.LeaseID})
	if err != nil {
		if contextErr := attemptCtx.Err(); contextErr != nil {
			return initializeOutcome{}, contextErr
		}
		return initializeOutcome{}, fmt.Errorf("release temporary Remote daemon probe lease: %w", err)
	}
	var detached protocol.DetachResult
	if err := decodeStrict(rawDetach, &detached); err != nil {
		return initializeOutcome{}, fmt.Errorf("decode Remote daemon detach result: %w", err)
	}
	if !detached.Detached {
		return initializeOutcome{}, errors.New("Remote daemon did not confirm probe lease detach")
	}
	return initializeOutcome{kind: initializeExact, buildID: result.BuildID}, nil
}

func validateInitializeResult(candidate protocol.BuildID, result protocol.InitializeResult) error {
	if err := result.BuildID.Validate(); err != nil {
		return fmt.Errorf("Remote daemon initialize returned invalid Build ID: %w", err)
	}
	if err := protocol.CompareBuildID(candidate, result.BuildID); err != nil {
		return errors.New("Remote daemon initialize result Build ID differs from the accepted candidate")
	}
	if strings.TrimSpace(string(result.HostEpoch)) == "" || strings.TrimSpace(string(result.Lease.LeaseID)) == "" {
		return errors.New("Remote daemon initialize result omitted Host or lease identity")
	}
	if result.Lease.TTLMillis != protocol.LeaseTTLMillis || result.Lease.PingIntervalMs != protocol.LeasePingIntervalMillis {
		return errors.New("Remote daemon initialize result used non-frozen lease timing")
	}
	if strings.TrimSpace(result.Host.OS) == "" || strings.TrimSpace(result.Host.Arch) == "" ||
		strings.TrimSpace(result.Host.ShellKind) == "" || strings.TrimSpace(result.Host.SandboxBackend) == "" {
		return errors.New("Remote daemon initialize result omitted Host information")
	}
	if err := result.Capabilities.Validate(); err != nil {
		return fmt.Errorf("Remote daemon initialize returned invalid capabilities: %w", err)
	}
	return nil
}

func decodeRemoteError(response *rpcwire.ResponseError) (protocol.RemoteErrorData, error) {
	if response == nil || response.Code != protocol.DomainErrorCode {
		return protocol.RemoteErrorData{}, errors.New("Remote daemon initialize returned a non-domain JSON-RPC error")
	}
	var data protocol.RemoteErrorData
	if err := decodeStrict(response.Data, &data); err != nil {
		return protocol.RemoteErrorData{}, fmt.Errorf("decode Remote daemon structured error: %w", err)
	}
	if err := data.Validate(); err != nil {
		return protocol.RemoteErrorData{}, fmt.Errorf("validate Remote daemon structured error: %w", err)
	}
	message := ""
	for _, contract := range protocol.ErrorContracts() {
		if contract.ReasonixCode == data.ReasonixCode {
			message = contract.Message
			break
		}
	}
	if message == "" || response.Message != message {
		return protocol.RemoteErrorData{}, errors.New("Remote daemon structured error message does not match the frozen error table")
	}
	return data, nil
}

func decodeStrict(raw json.RawMessage, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("missing JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func randomCandidate() (protocol.BuildID, error) {
	var entropy [64]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return protocol.BuildID{}, err
	}
	encoded := hex.EncodeToString(entropy[:])
	return protocol.BuildID{
		ProductVersion:  "reasonix-probe-product-" + encoded[:16],
		SourceRevision:  encoded[16:56],
		ProtocolVersion: "reasonix-probe-protocol-" + encoded[56:72],
		SchemaHash:      "sha256:" + encoded[64:128],
	}, nil
}

func randomClientID() (protocol.ClientInstanceID, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return protocol.ClientInstanceID("daemon_probe_" + hex.EncodeToString(entropy[:])), nil
}
