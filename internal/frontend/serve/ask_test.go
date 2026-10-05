package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// blockingAttacher holds a dial open and hands its context out, which is the
// only way to see whose lifetime the link layer is actually running under.
type blockingAttacher struct {
	dialing chan context.Context
}

func (b *blockingAttacher) Attach(ctx context.Context, _, _ string) (RemoteEndpoint, func(), error) {
	b.dialing <- ctx
	<-ctx.Done()
	return RemoteEndpoint{}, nil, ctx.Err()
}

func (b *blockingAttacher) Browse(ctx context.Context, _, _ string) (RemoteListing, error) {
	b.dialing <- ctx
	<-ctx.Done()
	return RemoteListing{}, ctx.Err()
}

func (b *blockingAttacher) States() map[string]RemoteLinkState { return nil }
func (b *blockingAttacher) Candidates() []string               { return nil }
func (b *blockingAttacher) Probe(context.Context, string) (RemoteProbe, error) {
	return RemoteProbe{}, nil
}

// Whose lifetime a remote dial runs under decides whether a question raised
// during it can outlive the client that asked. Established here rather than
// assumed, because the answer settles what a reload is allowed to recover.
func TestARemoteDialRunsUnderTheRequestThatAskedForIt(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	link := &blockingAttacher{dialing: make(chan context.Context, 1)}
	h := NewHub(HubOptions{Remote: link})
	front := httptest.NewServer(operatorHandler(h))
	defer front.Close()

	cases := []struct {
		name string
		call func(ctx context.Context) (*http.Request, error)
	}{
		{"opening a pane", func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, front.URL+"/remotes/open",
				strings.NewReader(`{"host":"gpu-box","workspace":"/srv/w"}`))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
			}
			return req, err
		}},
		{"browsing a machine", func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, front.URL+"/remotes/gpu-box/dirs", nil)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			req, err := c.call(ctx)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			go func() {
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					resp.Body.Close()
				}
			}()

			var dial context.Context
			select {
			case dial = <-link.dialing:
			case <-time.After(3 * time.Second):
				cancel()
				t.Fatal("the link layer was never reached")
			}
			if dial.Err() != nil {
				cancel()
				t.Fatal("the dial started already cancelled")
			}

			// The client goes away mid-dial, the way a reload does.
			cancel()
			select {
			case <-dial.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("the dial outlived the request that asked for it")
			}
		})
	}
}

// raise starts a question on its own goroutine and hands back the id once the
// broker is holding it, so a test can act on a question that is really open.
func raise(t *testing.T, b *AskBroker, ctx context.Context, operationID string) (string, chan AskAnswer) {
	t.Helper()
	done := make(chan AskAnswer, 1)
	go func() {
		answer, _ := b.Ask(WithOperationID(ctx, operationID), Ask{Kind: "hostkey", Host: "gpu-box"})
		done <- answer
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if open := b.Pending(operationID); len(open) > 0 {
			return open[0].ID, done
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the question never became visible")
	return "", nil
}

// Two dials at once are two questions, and a client polling its own must not be
// shown the other's — answering the wrong host key is the whole hazard.
func TestConcurrentOperationsDoNotSeeEachOthersQuestions(t *testing.T) {
	b := NewAskBroker(nil)
	first, _ := raise(t, b, t.Context(), "op-A")
	second, _ := raise(t, b, t.Context(), "op-B")

	onlyA := b.Pending("op-A")
	if len(onlyA) != 1 || onlyA[0].ID != first {
		t.Fatalf("op-A saw %+v, want only its own question", onlyA)
	}
	onlyB := b.Pending("op-B")
	if len(onlyB) != 1 || onlyB[0].ID != second {
		t.Fatalf("op-B saw %+v, want only its own question", onlyB)
	}
	if all := b.Pending(""); len(all) != 2 {
		t.Fatalf("the unnarrowed snapshot held %d, want both", len(all))
	}
	// And an answer aimed at the wrong operation does not land.
	if err := b.Answer(b.Epoch(), "op-B", first, AskAnswer{OK: true}); err == nil {
		t.Error("an answer reached a question belonging to another operation")
	}
}

// A restart numbers its questions afresh. An answer written for the run before
// must not land on whatever this one is asking.
func TestAnAnswerFromAnEarlierRunIsRefused(t *testing.T) {
	b := NewAskBroker(nil)
	id, _ := raise(t, b, t.Context(), "op")
	if err := b.Answer("epoch-from-a-previous-launch", "op", id, AskAnswer{OK: true}); !errors.Is(err, errAskStaleEpoch) {
		t.Fatalf("err = %v, want the stale epoch refusal", err)
	}
	if len(b.Pending("op")) != 1 {
		t.Error("the refused answer settled the question anyway")
	}
}

// A POST whose response was lost has to be safe to send again, and the client
// cannot know whether the first one landed. Repeating it succeeds; changing it
// is a second opinion and does not.
func TestAnswersAreIdempotentButNotRevisable(t *testing.T) {
	b := NewAskBroker(nil)
	id, done := raise(t, b, t.Context(), "op")
	answer := AskAnswer{OK: true, Text: "the passphrase"}

	if err := b.Answer(b.Epoch(), "op", id, answer); err != nil {
		t.Fatalf("first answer: %v", err)
	}
	if got := <-done; got != answer {
		t.Fatalf("the link layer got %+v, want %+v", got, answer)
	}
	if err := b.Answer(b.Epoch(), "op", id, answer); err != nil {
		t.Errorf("the same answer sent again: %v, want it to be accepted", err)
	}
	if err := b.Answer(b.Epoch(), "op", id, AskAnswer{OK: false}); !errors.Is(err, errAskAlreadyResolved) {
		t.Errorf("err = %v, want a different answer refused", err)
	}
}

// The operation is what the question belongs to, so when it ends the question
// goes with it. A late reply is told that rather than dropped, and rather than
// being confused with one that never existed.
func TestAQuestionEndsWithItsOperation(t *testing.T) {
	b := NewAskBroker(nil)
	ctx, cancel := context.WithCancel(context.Background())
	id, _ := raise(t, b, ctx, "op")

	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for len(b.Pending("op")) > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if open := b.Pending("op"); len(open) != 0 {
		t.Fatalf("the question outlived its operation: %+v", open)
	}
	if err := b.Answer(b.Epoch(), "op", id, AskAnswer{OK: true}); !errors.Is(err, errAskCancelled) {
		t.Fatalf("err = %v, want the cancelled refusal", err)
	}
	if err := b.Answer(b.Epoch(), "op", "ask-nothing-like-this", AskAnswer{OK: true}); !errors.Is(err, errAskNotFound) {
		t.Errorf("err = %v, want a question that never existed to say so", err)
	}
}

// The snapshot is the record and the wake-up is a convenience. A shell that
// missed the notification, or polled late, still finds the question.
func TestAMissedNotificationLosesNothing(t *testing.T) {
	// Carried rather than counted: the broker notifies after it lets go of the
	// question, so a poll that found it can still be ahead of the callback, and
	// a counter read here would be reading what that goroutine writes.
	woke := make(chan Ask, 4)
	b := NewAskBroker(func(q Ask) { woke <- q })
	id, _ := raise(t, b, t.Context(), "op")
	select {
	case <-woke:
	case <-time.After(3 * time.Second):
		t.Fatal("the shell was never woken for a question that is open")
	}
	// Two polls later — a client that slept through several rounds sees it just
	// the same, because nothing about the question was carried by the wake-up.
	for range 3 {
		if open := b.Pending("op"); len(open) != 1 || open[0].ID != id {
			t.Fatalf("a later poll saw %+v, want the question still open", open)
		}
	}
	// Once, not once-so-far: polling is not what raises a question, so nothing
	// after the first wake-up may have produced another.
	select {
	case q := <-woke:
		t.Fatalf("the shell was woken again, for %+v", q)
	default:
	}
}

// The endpoints answer with the same identities the broker refuses on, so a
// client can tell the four apart without reading a sentence.
func TestTheAnswerEndpointNamesWhatWentWrong(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	b := NewAskBroker(nil)
	h := NewHub(HubOptions{Asks: b})
	front := httptest.NewServer(operatorHandler(h))
	defer front.Close()

	ctx, cancel := context.WithCancel(context.Background())
	id, _ := raise(t, b, ctx, "op")

	post := func(t *testing.T, askID, body string) (int, string) {
		t.Helper()
		resp, err := http.Post(front.URL+"/asks/"+askID+"/answer", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := make([]byte, 2048)
		n, _ := resp.Body.Read(out)
		return resp.StatusCode, string(out[:n])
	}

	if status, body := post(t, id, `{"epoch":"someone-elses","operationId":"op","answer":{"ok":true}}`); !strings.Contains(body, codeAskStaleEpoch) {
		t.Errorf("stale epoch = %d %s, want %s", status, body, codeAskStaleEpoch)
	}
	if status, body := post(t, "ask-nothing", `{"operationId":"op","answer":{"ok":true}}`); !strings.Contains(body, codeAskNotFound) {
		t.Errorf("unknown question = %d %s, want %s", status, body, codeAskNotFound)
	}
	if status, _ := post(t, id, `{"epoch":"`+b.Epoch()+`","operationId":"op","answer":{"ok":true}}`); status != http.StatusNoContent {
		t.Errorf("a good answer = %d, want 204", status)
	}
	if status, body := post(t, id, `{"epoch":"`+b.Epoch()+`","operationId":"op","answer":{"ok":false}}`); !strings.Contains(body, codeAskAlreadyResolved) {
		t.Errorf("a second opinion = %d %s, want %s", status, body, codeAskAlreadyResolved)
	}
	cancel()
}

// askingAttacher stops on the first dial the way a first-seen host key does,
// and goes on once somebody answers.
type askingAttacher struct {
	asks     *AskBroker
	endpoint RemoteEndpoint
}

func (a *askingAttacher) Attach(ctx context.Context, host, workspace string) (RemoteEndpoint, func(), error) {
	answer, err := a.asks.Ask(ctx, Ask{Kind: "hostkey", Host: host, Fingerprint: "SHA256:whatever"})
	if err != nil {
		return RemoteEndpoint{}, nil, err
	}
	if !answer.OK {
		return RemoteEndpoint{}, nil, errors.New("the key was refused")
	}
	ep := a.endpoint
	ep.Host, ep.Workspace = host, workspace
	return ep, func() {}, nil
}

func (a *askingAttacher) Browse(context.Context, string, string) (RemoteListing, error) {
	return RemoteListing{}, nil
}
func (a *askingAttacher) States() map[string]RemoteLinkState { return nil }
func (a *askingAttacher) Candidates() []string               { return nil }
func (a *askingAttacher) Probe(context.Context, string) (RemoteProbe, error) {
	return RemoteProbe{}, nil
}

// The chain a person actually walks: open a workspace on another machine, be
// stopped by a question, find it by polling the operation, answer it, and have
// the pane come up. Everything but the SSH itself, driven over HTTP the way any
// shell drives it.
func TestAnsweringAQuestionLetsTheDialFinishAndThePaneOpen(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	far := fakeRemoteKernel(t)
	asks := NewAskBroker(nil)
	h := NewHub(HubOptions{
		Asks:   asks,
		Remote: &askingAttacher{asks: asks, endpoint: RemoteEndpoint{Addr: far.Listener.Addr().String(), Token: "remote-secret"}},
	})
	front := httptest.NewServer(operatorHandler(h))
	defer front.Close()

	const operation = "op-the-client-named"
	opened := make(chan *http.Response, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, front.URL+"/remotes/open",
			strings.NewReader(`{"host":"gpu-box","workspace":"/srv/w"}`))
		if err != nil {
			opened <- nil
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(operationHeader, operation)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			opened <- nil
			return
		}
		opened <- resp
	}()

	// The client polls its own operation, which is the only thing it has to go
	// on: the response it is waiting for is what the question is blocking.
	var found Ask
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(front.URL + "/asks?operationId=" + operation)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Epoch string `json:"epoch"`
			Asks  []Ask  `json:"asks"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if len(body.Asks) == 1 {
			found = body.Asks[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if found.ID == "" {
		t.Fatal("the question never surfaced to the client that raised it")
	}
	if found.OperationID != operation || found.Epoch != asks.Epoch() {
		t.Fatalf("ask = %+v, want it to name this operation and this launch", found)
	}

	answer, err := http.Post(front.URL+"/asks/"+found.ID+"/answer", "application/json",
		strings.NewReader(`{"epoch":"`+found.Epoch+`","operationId":"`+operation+`","answer":{"ok":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	answer.Body.Close()
	if answer.StatusCode != http.StatusNoContent {
		t.Fatalf("answering = %d, want 204", answer.StatusCode)
	}

	resp := <-opened
	if resp == nil {
		t.Fatal("the open never came back")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		t.Fatalf("open = %d %s, want the pane to have come up", resp.StatusCode, body[:n])
	}
	var view RuntimeView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.Host != "gpu-box" || view.Base == "" {
		t.Fatalf("pane = %+v, want a remote runtime on the machine that was asked for", view)
	}
	// And the pane's stream is the proxy's, not something a shell arranged.
	streamCtx, stopStream := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, front.URL+view.Base+"/events", nil)
	if err != nil {
		stopStream()
		t.Fatal(err)
	}
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		stopStream()
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stream.Body).ReadString('\n')
	stream.Body.Close()
	// Let go of both ends before the servers come down: a proxy still holding
	// a stream open is what makes a close wait rather than a test fail.
	stopStream()
	close(far.release)
	if err != nil || !strings.Contains(line, "first") {
		t.Fatalf("the pane's first frame was %q (%v), want the far kernel's", line, err)
	}
}
