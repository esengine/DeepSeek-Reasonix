// Package attach implements the short-lived stdio bootstrap used after SSH
// starts `reasonix remote attach --stdio`. It validates the Desktop build before
// observing service state, then becomes a byte-transparent Unix-socket proxy.
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

	"reasonix/internal/nilutil"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

// Service is the deliberately small boundary between the platform-neutral
// attach bootstrap and a platform-specific user service endpoint.
type Service interface {
	Installed(context.Context) (bool, error)
	Dial(context.Context) (net.Conn, error)
}

type Options struct {
	BuildID protocol.BuildID
	Service Service
	// OnDiagnostic receives local service/path failures. It must never write to
	// the protocol stdout; the CLI may route it to a structured host log.
	OnDiagnostic func(error)
}

// Run validates one initialize request, connects to the already-running Host,
// forwards the exact bootstrap bytes, and proxies both directions until EOF.
// It never installs, starts, upgrades, synchronizes, or repairs the service.
func Run(ctx context.Context, stdin io.ReadCloser, stdout io.Writer, options Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if nilutil.IsNil(stdin) || nilutil.IsNil(stdout) {
		return errors.New("remote attach requires stdio")
	}
	if err := options.BuildID.Validate(); err != nil {
		return fmt.Errorf("remote attach Build ID: %w", err)
	}
	if nilutil.IsNil(options.Service) {
		return errors.New("remote attach service is required")
	}

	reader := bufio.NewReaderSize(stdin, 64<<10)
	// Bootstrap has not entered proxy's cancellation loop yet. Closing stdin is
	// the only portable way to interrupt a blocked first-frame read when the CLI
	// receives SIGTERM or its caller cancels the attachment.
	stopBootstrapCancellation := context.AfterFunc(ctx, func() { _ = stdin.Close() })
	frame, err := rpcwire.ReadStrictRequestFrame(reader, protocol.FrameBytes)
	stopBootstrapCancellation()
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if err != nil {
		var tooLarge *rpcwire.FrameTooLargeError
		if errors.As(err, &tooLarge) {
			// A framing violation is terminal. In particular, do not emit an
			// error frame after consuming only a prefix of an oversized request.
			return err
		}
		if len(frame.Raw) != 0 {
			code := rpcwire.ErrInvalidRequest
			message := "invalid request"
			if !json.Valid(bytes.TrimSpace(frame.Raw)) {
				code = rpcwire.ErrParse
				message = "parse error"
			}
			return writeRPCError(stdout, rpcwire.ResponseIDForError(frame.ID), &rpcwire.RPCError{Code: code, Message: message})
		}
		return fmt.Errorf("remote attach bootstrap: %w", err)
	}
	if protocol.Method(frame.Method) != protocol.MethodRemoteInitialize {
		return writeRPCError(stdout, frame.ID, &rpcwire.RPCError{
			Code: rpcwire.ErrInvalidRequest, Message: "remote/initialize must be the first request",
		})
	}
	decoded, err := protocol.DecodeRequestParams(protocol.MethodRemoteInitialize, frame.Params)
	if err != nil {
		return writeRPCError(stdout, frame.ID, &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "invalid params"})
	}
	params, ok := decoded.(protocol.InitializeParams)
	if !ok {
		return errors.New("remote attach initialize registry returned an unexpected type")
	}
	if err := protocol.CompareBuildID(options.BuildID, params.BuildID); err != nil {
		var mismatch *protocol.BuildIDMismatch
		if !errors.As(err, &mismatch) {
			return err
		}
		return writeRemoteError(stdout, frame.ID, protocol.MustRemoteError(protocol.ErrVersionMismatch, protocol.ErrorOptions{
			Expected: mismatch.Expected, Actual: mismatch.Actual,
		}))
	}

	installed, err := options.Service.Installed(ctx)
	if err != nil {
		report(options, err)
		return writeRemoteError(stdout, frame.ID, protocol.MustRemoteError(protocol.ErrHostStopped, protocol.ErrorOptions{}))
	}
	if !installed {
		return writeRemoteError(stdout, frame.ID, protocol.MustRemoteError(protocol.ErrRemoteNotInstalled, protocol.ErrorOptions{}))
	}
	connection, err := options.Service.Dial(ctx)
	if err != nil {
		report(options, err)
		return writeRemoteError(stdout, frame.ID, protocol.MustRemoteError(protocol.ErrHostStopped, protocol.ErrorOptions{}))
	}
	if connection == nil {
		err = errors.New("remote attach service returned a nil connection")
		report(options, err)
		return writeRemoteError(stdout, frame.ID, protocol.MustRemoteError(protocol.ErrHostStopped, protocol.ErrorOptions{}))
	}
	defer connection.Close()

	if err := writeAll(connection, frame.Raw); err != nil {
		return fmt.Errorf("forward remote initialize: %w", err)
	}
	return proxy(ctx, stdin, reader, stdout, connection)
}

func report(options Options, err error) {
	if options.OnDiagnostic != nil && err != nil {
		options.OnDiagnostic(err)
	}
}

func writeRemoteError(writer io.Writer, id json.RawMessage, remote *protocol.RemoteError) error {
	return writeRPCError(writer, id, remote.RPCError())
}

func writeRPCError(writer io.Writer, id json.RawMessage, rpcError *rpcwire.RPCError) error {
	if rpcError == nil {
		return errors.New("remote attach cannot write a nil RPC error")
	}
	var data json.RawMessage
	if rpcError.Data != nil {
		encoded, err := json.Marshal(rpcError.Data)
		if err != nil {
			return err
		}
		if string(encoded) != "null" {
			data = encoded
		}
	}
	response := struct {
		JSONRPC string               `json:"jsonrpc"`
		ID      json.RawMessage      `json:"id"`
		Error   *rpcwire.ErrorObject `json:"error"`
	}{
		JSONRPC: "2.0", ID: rpcwire.ResponseIDForError(id),
		Error: &rpcwire.ErrorObject{Code: rpcError.Code, Message: rpcError.Message, Data: data},
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(response); err != nil {
		return err
	}
	if buffer.Len() > protocol.FrameBytes {
		return &rpcwire.FrameTooLargeError{Direction: "outbound", Size: buffer.Len(), Limit: protocol.FrameBytes}
	}
	return writeAll(writer, buffer.Bytes())
}

func writeAll(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		value = value[written:]
	}
	return nil
}

type copyDirection uint8

const (
	copyToDaemon copyDirection = iota
	copyToDesktop
)

type copyResult struct {
	direction copyDirection
	err       error
}

func proxy(ctx context.Context, stdin io.ReadCloser, input io.Reader, stdout io.Writer, connection net.Conn) error {
	results := make(chan copyResult, 2)
	go func() {
		_, err := io.Copy(connection, input)
		if err == nil {
			if closeWriter, ok := connection.(interface{ CloseWrite() error }); ok {
				_ = closeWriter.CloseWrite()
			}
		}
		results <- copyResult{direction: copyToDaemon, err: err}
	}()
	go func() {
		_, err := io.Copy(stdout, connection)
		results <- copyResult{direction: copyToDesktop, err: err}
	}()

	inputDone := false
	outputDone := false
	terminating := false
	var inputErr error
	var outputErr error
	var contextErr error
	ctxDone := ctx.Done()
	terminate := func() {
		if terminating {
			return
		}
		terminating = true
		_ = connection.Close()
		_ = stdin.Close()
	}

	for !inputDone || !outputDone {
		select {
		case <-ctxDone:
			contextErr = ctx.Err()
			ctxDone = nil
			terminate()
		case result := <-results:
			switch result.direction {
			case copyToDaemon:
				inputDone = true
				if result.err != nil && !terminating {
					inputErr = result.err
					terminate()
				}
			case copyToDesktop:
				outputDone = true
				if result.err != nil && !terminating {
					outputErr = result.err
				}
				// Host EOF is terminal for attach. Closing stdin unblocks an SSH
				// input read without making daemon work connection-owned.
				terminate()
			}
		}
	}
	if outputErr != nil {
		return fmt.Errorf("proxy daemon to Desktop: %w", outputErr)
	}
	if inputErr != nil {
		return fmt.Errorf("proxy Desktop to daemon: %w", inputErr)
	}
	if contextErr != nil && !errors.Is(contextErr, context.Canceled) {
		return contextErr
	}
	return nil
}
