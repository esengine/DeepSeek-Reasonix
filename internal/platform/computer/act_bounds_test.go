package computer

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// failingWrite accepts a deadline and then refuses the write, the half of the
// abort exchange a pipe that is closed outright never reaches.
type failingWrite struct{ net.Conn }

func (failingWrite) Write([]byte) (int, error) { return 0, errors.New("write refused") }

func TestCUAAbortExchangeReportsATransportThatFails(t *testing.T) {
	closed, peer := net.Pipe()
	_ = peer.Close()
	_ = closed.Close()
	if err := exchangeAbort(closed); err == nil {
		t.Fatal("a closed connection accepted the abort")
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	if err := exchangeAbort(failingWrite{client}); err == nil {
		t.Fatal("a refused write was taken for an abort")
	}
	if err := abortEmbedded(testenv.TempDir(t) + "/no-such.sock"); err == nil {
		t.Fatal("an absolute path with nothing listening was taken for an endpoint")
	}
}

func TestCUAActRefusesAWindowTheApplicationDoesNotHave(t *testing.T) {
	s := fakeSession(t)
	result, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "key", Key: "Enter"}}, 9999)
	if CodeOf(err) != CodeNoWindow || result.FailedAt != 0 || result.Done != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}

// expiring reports its context as cancelled once Err has been asked n times, so
// a batch can be stopped between two of its steps rather than before the first.
type expiring struct {
	context.Context
	left int
}

func (e *expiring) Err() error {
	if e.left > 0 {
		e.left--
		return nil
	}
	return context.Canceled
}

func TestCUAActStopsBetweenStepsWhenTheContextEnds(t *testing.T) {
	for left := range 40 {
		s := fakeSession(t)
		ref := snapshotRef(t, s)
		result, err := s.Act(&expiring{Context: t.Context(), left: left}, "com.example.Notes", []Step{{Action: "click", Ref: ref}, {Action: "type", Text: "never"}})
		if errors.Is(err, context.Canceled) && result.Done == 1 && result.FailedAt == 1 {
			if s.state.refs != nil {
				t.Fatal("a stopped batch kept the refs of the page it left")
			}
			return
		}
	}
	t.Fatal("no cancellation point fell between the two steps")
}

func TestCUAActionContractRefusesANegativeDeliveryCount(t *testing.T) {
	count := -1
	action := DriverAction{Effect: "confirmed", Route: "accessibility"}
	action.Delivery = &struct {
		Mode  string `json:"mode"`
		Count *int   `json:"delivered_count"`
	}{Mode: "background", Count: &count}
	if _, err := finishAction(action); CodeOf(err) != CodeFailed {
		t.Fatalf("negative count accepted: %v", err)
	}
	action.Delivery.Mode = "sideways"
	if _, err := finishAction(action); CodeOf(err) != CodeFailed {
		t.Fatalf("unknown delivery mode accepted: %v", err)
	}
}

func TestCUAAbortEmbeddedStopsThroughALiveEndpoint(t *testing.T) {
	dir, err := os.MkdirTemp("", "cua")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "a.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("no unix sockets here: %v", err)
	}
	defer listener.Close()
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = bufio.NewReader(connection).ReadString('\n')
		_, _ = connection.Write([]byte("{\"ok\":true}\n"))
	}()
	if err := abortEmbedded(socket); err != nil {
		t.Fatalf("a live endpoint that acknowledged the stop: %v", err)
	}
}
