//go:build windows

package instanceidentity

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"golang.org/x/sys/windows"
)

func endpointName(id string) (*uint16, error) {
	if !Valid(id) {
		return nil, fmt.Errorf("invalid desktop instance identity")
	}
	return windows.UTF16PtrFromString(`\\.\pipe\reasonix-desktop-` + id)
}

// ListenEndpoint exposes a kernel-owned service PID to the updater. No PID file
// can outlive a generation or be confused with a recycled process. It carries
// no business RPC and rejects remote clients.
func ListenEndpoint(id string) (func(), error) {
	name, err := endpointName(id)
	if err != nil {
		return nil, err
	}
	pipe, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE, windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 1, 1, 0, nil)
	if err != nil {
		return nil, err
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(pipe)
		return nil, err
	}
	done := make(chan struct{})
	var mu sync.Mutex
	stopped := false
	go func() {
		defer close(done)
		wait := func(err error, ov *windows.Overlapped) bool {
			if !errors.Is(err, windows.ERROR_IO_PENDING) {
				return err == nil || errors.Is(err, windows.ERROR_PIPE_CONNECTED)
			}
			_, err = windows.WaitForSingleObject(event, windows.INFINITE)
			if err != nil {
				return false
			}
			var transferred uint32
			return windows.GetOverlappedResult(pipe, ov, &transferred, false) == nil
		}
		submit := func(call func(*windows.Overlapped) error) bool {
			mu.Lock()
			if stopped {
				mu.Unlock()
				return false
			}
			if err := windows.ResetEvent(event); err != nil {
				mu.Unlock()
				slog.Warn("desktop identity endpoint: reset completion event", "err", err)
				return false
			}
			ov := windows.Overlapped{HEvent: event}
			err := call(&ov)
			mu.Unlock()
			return wait(err, &ov)
		}
		for {
			if !submit(func(ov *windows.Overlapped) error { return windows.ConnectNamedPipe(pipe, ov) }) {
				return
			}
			var buf [1]byte
			var read uint32
			// The client acknowledges only after querying its server PID, so the
			// connection remains bound during identity inspection.
			submit(func(ov *windows.Overlapped) error { return windows.ReadFile(pipe, buf[:], &read, ov) })
			if err := windows.DisconnectNamedPipe(pipe); err != nil && !errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED) {
				slog.Warn("desktop identity endpoint: disconnect client", "err", err)
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			stopped = true
			// No pending I/O is normal between submissions. On any other
			// failure, retain the handles and OVERLAPPED until the worker
			// finishes; cancellation failure cannot prove I/O completion.
			if err := windows.CancelIoEx(pipe, nil); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
				slog.Warn("desktop identity endpoint: cancel pending I/O", "err", err)
			}
			mu.Unlock()
			<-done
			windows.CloseHandle(pipe)
			windows.CloseHandle(event)
		})
	}, nil
}

// EndpointImage queries the server process through an actual local pipe handle.
// An absent endpoint means no Electron service owns this data home.
func EndpointImage(id string) (string, error) {
	name, err := endpointName(id)
	if err != nil {
		return "", err
	}
	pipe, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(pipe)
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(pipe, &pid); err != nil {
		return "", err
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(process)
	size := uint32(32768)
	buffer := make([]uint16, size)
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	state, err := windows.WaitForSingleObject(process, 0)
	if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		return "", fmt.Errorf("desktop endpoint process exited")
	}
	var written uint32
	if err := windows.WriteFile(pipe, []byte{1}, &written, nil); err != nil {
		return "", fmt.Errorf("desktop endpoint closed during inspection: %w", err)
	}
	if written != 1 {
		return "", fmt.Errorf("desktop endpoint acknowledgement: %w", io.ErrShortWrite)
	}
	return windows.UTF16ToString(buffer[:size]), nil
}
