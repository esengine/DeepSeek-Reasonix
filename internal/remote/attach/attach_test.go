package attach

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

const attachTestTimeout = 3 * time.Second

type fakeService struct {
	installed      bool
	installedErr   error
	dialErr        error
	connection     net.Conn
	installedCalls atomic.Int32
	dialCalls      atomic.Int32
}

func (s *fakeService) Installed(context.Context) (bool, error) {
	s.installedCalls.Add(1)
	return s.installed, s.installedErr
}

func (s *fakeService) Dial(context.Context) (net.Conn, error) {
	s.dialCalls.Add(1)
	return s.connection, s.dialErr
}

func attachTestBuildID(t *testing.T, revision byte) protocol.BuildID {
	t.Helper()
	id, err := protocol.NewBuildID("v-test", strings.Repeat(string(revision), 40))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func initializeFrame(t *testing.T, id string, buildID protocol.BuildID, suffix string) []byte {
	t.Helper()
	build, err := json.Marshal(buildID)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(fmt.Sprintf(" \t{\"jsonrpc\":\"2.0\",\"id\":%s,\"method\":\"remote/initialize\",\"params\":{\"buildId\":%s,\"clientInstanceId\":\"desktop-test\"}}%s", id, build, suffix))
}

type bootstrapErrorResponse struct {
	JSONRPC string              `json:"jsonrpc"`
	ID      json.RawMessage     `json:"id"`
	Error   rpcwire.ErrorObject `json:"error"`
}

func decodeBootstrapError(t *testing.T, output []byte) bootstrapErrorResponse {
	t.Helper()
	var response bootstrapErrorResponse
	decoder := json.NewDecoder(bytes.NewReader(output))
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode bootstrap response: %v\n%s", err, output)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("bootstrap wrote more than one frame: %v\n%s", err, output)
	}
	return response
}

func decodeRemoteErrorData(t *testing.T, response bootstrapErrorResponse) protocol.RemoteErrorData {
	t.Helper()
	if response.Error.Code != protocol.DomainErrorCode {
		t.Fatalf("JSON-RPC code = %d, want %d", response.Error.Code, protocol.DomainErrorCode)
	}
	var data protocol.RemoteErrorData
	if err := json.Unmarshal(response.Error.Data, &data); err != nil {
		t.Fatalf("decode remote error: %v", err)
	}
	if err := data.Validate(); err != nil {
		t.Fatalf("invalid remote error: %v", err)
	}
	return data
}

func TestBuildMismatchPrecedesEveryServiceCheckAndPreservesID(t *testing.T) {
	service := &fakeService{installed: true, dialErr: errors.New("must not dial")}
	attachBuild := attachTestBuildID(t, 'a')
	desktopBuild := attachTestBuildID(t, 'b')
	input := initializeFrame(t, `"desktop-request-7"`, desktopBuild, "\r\n")
	var output bytes.Buffer

	if err := Run(context.Background(), io.NopCloser(bytes.NewReader(input)), &output, Options{
		BuildID: attachBuild, Service: service,
	}); err != nil {
		t.Fatal(err)
	}
	response := decodeBootstrapError(t, output.Bytes())
	if string(response.ID) != `"desktop-request-7"` {
		t.Fatalf("response id = %s", response.ID)
	}
	data := decodeRemoteErrorData(t, response)
	if data.ReasonixCode != protocol.ErrVersionMismatch {
		t.Fatalf("reasonixCode = %q", data.ReasonixCode)
	}
	if data.Expected != strings.Repeat("a", 40) || data.Actual != strings.Repeat("b", 40) {
		t.Fatalf("mismatch orientation = expected %q actual %q", data.Expected, data.Actual)
	}
	if service.installedCalls.Load() != 0 || service.dialCalls.Load() != 0 {
		t.Fatalf("service observed before Build check: installed=%d dial=%d", service.installedCalls.Load(), service.dialCalls.Load())
	}
}

func TestBootstrapDistinguishesNotInstalledAndStopped(t *testing.T) {
	buildID := attachTestBuildID(t, 'a')
	tests := []struct {
		name          string
		service       *fakeService
		want          protocol.ReasonixErrorCode
		wantDialCalls int32
	}{
		{name: "not installed", service: &fakeService{}, want: protocol.ErrRemoteNotInstalled},
		{name: "stopped", service: &fakeService{installed: true, dialErr: errors.New("connection refused")}, want: protocol.ErrHostStopped, wantDialCalls: 1},
		{name: "probe failure", service: &fakeService{installedErr: errors.New("unit unreadable")}, want: protocol.ErrHostStopped},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			diagnostics := atomic.Int32{}
			err := Run(context.Background(), io.NopCloser(bytes.NewReader(initializeFrame(t, "19", buildID, "\n"))), &output, Options{
				BuildID: buildID, Service: test.service,
				OnDiagnostic: func(error) { diagnostics.Add(1) },
			})
			if err != nil {
				t.Fatal(err)
			}
			response := decodeBootstrapError(t, output.Bytes())
			if string(response.ID) != "19" {
				t.Fatalf("response id = %s", response.ID)
			}
			if data := decodeRemoteErrorData(t, response); data.ReasonixCode != test.want {
				t.Fatalf("reasonixCode = %q, want %q", data.ReasonixCode, test.want)
			}
			if test.service.installedCalls.Load() != 1 || test.service.dialCalls.Load() != test.wantDialCalls {
				t.Fatalf("service calls installed=%d dial=%d", test.service.installedCalls.Load(), test.service.dialCalls.Load())
			}
			wantDiagnostics := int32(0)
			if test.service.installedErr != nil || test.service.dialErr != nil {
				wantDiagnostics = 1
			}
			if diagnostics.Load() != wantDiagnostics {
				t.Fatalf("diagnostics = %d, want %d", diagnostics.Load(), wantDiagnostics)
			}
		})
	}
}

func TestBootstrapRejectsNonInitializeAndInvalidParamsBeforeService(t *testing.T) {
	buildID := attachTestBuildID(t, 'a')
	tests := []struct {
		name     string
		input    string
		wantCode int
	}{
		{name: "ping first", input: `{"jsonrpc":"2.0","id":"wrong-order","method":"remote/ping","params":{"leaseId":"lease-test"}}` + "\n", wantCode: rpcwire.ErrInvalidRequest},
		{name: "invalid initialize", input: `{"jsonrpc":"2.0","id":23,"method":"remote/initialize","params":{"clientInstanceId":"desktop-test"}}` + "\n", wantCode: rpcwire.ErrInvalidParams},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeService{installed: true}
			var output bytes.Buffer
			if err := Run(context.Background(), io.NopCloser(strings.NewReader(test.input)), &output, Options{BuildID: buildID, Service: service}); err != nil {
				t.Fatal(err)
			}
			response := decodeBootstrapError(t, output.Bytes())
			if response.Error.Code != test.wantCode {
				t.Fatalf("error code = %d, want %d", response.Error.Code, test.wantCode)
			}
			if service.installedCalls.Load() != 0 || service.dialCalls.Load() != 0 {
				t.Fatal("invalid bootstrap observed service state")
			}
		})
	}
}

func TestMalformedAndStrictInvalidFirstFrameReceiveStandardErrors(t *testing.T) {
	buildID := attachTestBuildID(t, 'a')
	tests := []struct {
		name     string
		input    string
		wantID   string
		wantCode int
	}{
		{name: "malformed", input: "{not-json\n", wantID: "null", wantCode: rpcwire.ErrParse},
		{name: "wrong jsonrpc", input: `{"jsonrpc":"1.0","id":"strict-id","method":"remote/initialize","params":{}}` + "\n", wantID: `"strict-id"`, wantCode: rpcwire.ErrInvalidRequest},
		{name: "notification", input: `{"jsonrpc":"2.0","method":"remote/initialize","params":{}}` + "\n", wantID: "null", wantCode: rpcwire.ErrInvalidRequest},
		{name: "object id", input: `{"jsonrpc":"2.0","id":{"unsafe":true},"method":"remote/initialize","params":{}}` + "\n", wantID: "null", wantCode: rpcwire.ErrInvalidRequest},
		{name: "array id", input: `{"jsonrpc":"2.0","id":[1],"method":"remote/initialize","params":{}}` + "\n", wantID: "null", wantCode: rpcwire.ErrInvalidRequest},
		{name: "boolean id", input: `{"jsonrpc":"2.0","id":true,"method":"remote/initialize","params":{}}` + "\n", wantID: "null", wantCode: rpcwire.ErrInvalidRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeService{installed: true}
			var output bytes.Buffer
			if err := Run(context.Background(), io.NopCloser(strings.NewReader(test.input)), &output, Options{BuildID: buildID, Service: service}); err != nil {
				t.Fatal(err)
			}
			response := decodeBootstrapError(t, output.Bytes())
			if string(response.ID) != test.wantID || response.Error.Code != test.wantCode {
				t.Fatalf("response id/code = %s/%d, want %s/%d", response.ID, response.Error.Code, test.wantID, test.wantCode)
			}
			if service.installedCalls.Load() != 0 || service.dialCalls.Load() != 0 {
				t.Fatal("invalid first frame observed service state")
			}
		})
	}
}

func TestOversizedFirstFrameIsTerminalAndWritesNothing(t *testing.T) {
	buildID := attachTestBuildID(t, 'a')
	service := &fakeService{installed: true}
	input := io.NopCloser(strings.NewReader(strings.Repeat("x", protocol.FrameBytes+1)))
	var output bytes.Buffer
	err := Run(context.Background(), input, &output, Options{BuildID: buildID, Service: service})
	var tooLarge *rpcwire.FrameTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("oversized first frame error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("oversized first frame wrote %d protocol bytes", output.Len())
	}
	if service.installedCalls.Load() != 0 || service.dialCalls.Load() != 0 {
		t.Fatal("oversized first frame observed service state")
	}
}

func TestCancellationInterruptsBlockedBootstrapRead(t *testing.T) {
	buildID := attachTestBuildID(t, 'a')
	attachInput, desktopInput := net.Pipe()
	defer desktopInput.Close()
	service := &fakeService{installed: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, attachInput, io.Discard, Options{BuildID: buildID, Service: service})
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled bootstrap error = %v", err)
		}
	case <-time.After(attachTestTimeout):
		t.Fatal("cancellation did not interrupt the blocked bootstrap read")
	}
	if service.installedCalls.Load() != 0 || service.dialCalls.Load() != 0 {
		t.Fatal("cancelled bootstrap observed service state")
	}
}

func TestBootstrapErrorHonorsOutboundFrameLimitBeforeWriting(t *testing.T) {
	var output bytes.Buffer
	err := writeRPCError(&output, json.RawMessage("1"), &rpcwire.RPCError{
		Code: rpcwire.ErrInternal, Message: "oversized", Data: strings.Repeat("x", protocol.FrameBytes),
	})
	var tooLarge *rpcwire.FrameTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("oversized bootstrap error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("oversized bootstrap wrote %d bytes", output.Len())
	}
}

func TestRunRejectsTypedNilStdioAndService(t *testing.T) {
	buildID := attachTestBuildID(t, 'a')
	var nilService *fakeService
	if err := Run(context.Background(), io.NopCloser(strings.NewReader("")), io.Discard, Options{
		BuildID: buildID, Service: nilService,
	}); err == nil || !strings.Contains(err.Error(), "service is required") {
		t.Fatalf("typed-nil service error = %v", err)
	}

	var nilInput *typedNilReadCloser
	if err := Run(context.Background(), nilInput, io.Discard, Options{
		BuildID: buildID, Service: &fakeService{},
	}); err == nil || !strings.Contains(err.Error(), "requires stdio") {
		t.Fatalf("typed-nil stdin error = %v", err)
	}

	var nilOutput *typedNilWriter
	if err := Run(context.Background(), io.NopCloser(strings.NewReader("")), nilOutput, Options{
		BuildID: buildID, Service: &fakeService{},
	}); err == nil || !strings.Contains(err.Error(), "requires stdio") {
		t.Fatalf("typed-nil stdout error = %v", err)
	}
}

type typedNilReadCloser struct{}

func (*typedNilReadCloser) Read([]byte) (int, error) { panic("typed-nil stdin was used") }
func (*typedNilReadCloser) Close() error             { panic("typed-nil stdin was closed") }

type typedNilWriter struct{}

func (*typedNilWriter) Write([]byte) (int, error) { panic("typed-nil stdout was used") }

func TestProxyForwardsExactInitializeAndBufferedRemainder(t *testing.T) {
	buildID := attachTestBuildID(t, 'a')
	attachSide, daemonSide := net.Pipe()
	service := &fakeService{installed: true, connection: attachSide}
	first := initializeFrame(t, "1", buildID, "\r\n")
	second := []byte("{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"remote/ping\",\"params\":{\"leaseId\":\"lease-test\"}}\n")
	input := append(append([]byte(nil), first...), second...)
	var output bytes.Buffer
	daemonResult := make(chan error, 1)
	go func() {
		defer daemonSide.Close()
		reader := bufio.NewReader(daemonSide)
		gotFirst, err := reader.ReadString('\n')
		if err != nil {
			daemonResult <- err
			return
		}
		if gotFirst != string(first) {
			daemonResult <- fmt.Errorf("first frame changed\n got: %q\nwant: %q", gotFirst, first)
			return
		}
		gotSecond, err := reader.ReadString('\n')
		if err != nil {
			daemonResult <- err
			return
		}
		if gotSecond != string(second) {
			daemonResult <- fmt.Errorf("buffered frame changed\n got: %q\nwant: %q", gotSecond, second)
			return
		}
		_, err = io.WriteString(daemonSide, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{}}\n")
		daemonResult <- err
	}()

	if err := Run(context.Background(), io.NopCloser(bytes.NewReader(input)), &output, Options{BuildID: buildID, Service: service}); err != nil {
		t.Fatal(err)
	}
	if err := <-daemonResult; err != nil {
		t.Fatal(err)
	}
	wantOutput := "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{}}\n"
	if output.String() != wantOutput {
		t.Fatalf("daemon output changed\n got: %q\nwant: %q", output.String(), wantOutput)
	}
}

func TestProxyIsFullDuplexAndDaemonEOFUnblocksOpenStdin(t *testing.T) {
	buildID := attachTestBuildID(t, 'a')
	attachInput, desktopInput := net.Pipe()
	attachSide, daemonSide := net.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	service := &fakeService{installed: true, connection: attachSide}
	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(context.Background(), attachInput, stdoutWriter, Options{BuildID: buildID, Service: service})
		_ = stdoutWriter.Close()
	}()

	first := initializeFrame(t, "5", buildID, "\n")
	writeDone := make(chan error, 1)
	go func() {
		_, err := desktopInput.Write(first)
		writeDone <- err
	}()
	daemonReader := bufio.NewReader(daemonSide)
	if got, err := daemonReader.ReadString('\n'); err != nil || got != string(first) {
		t.Fatalf("daemon initialize = %q, %v", got, err)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}

	response := "{\"jsonrpc\":\"2.0\",\"id\":5,\"result\":{\"ready\":true}}\n"
	go func() {
		_, err := io.WriteString(daemonSide, response)
		writeDone <- err
	}()
	if got, err := bufio.NewReader(stdoutReader).ReadString('\n'); err != nil || got != response {
		t.Fatalf("Desktop response before input EOF = %q, %v", got, err)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}

	// The Desktop input remains open. Host EOF must still end attach and close
	// the blocked stdio read instead of leaking a proxy goroutine.
	_ = daemonSide.Close()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(attachTestTimeout):
		t.Fatal("daemon EOF did not stop attach")
	}
	if _, err := desktopInput.Write([]byte("late")); err == nil {
		t.Fatal("daemon EOF did not close the attach stdin side")
	}
	_ = desktopInput.Close()
	_ = stdoutReader.Close()
}
