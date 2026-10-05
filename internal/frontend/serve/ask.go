package serve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// askPatience bounds a question nobody answers. The operation is blocked while
// it waits, and a client that went away without cancelling would otherwise hold
// the dial open until the process ends.
const askPatience = 3 * time.Minute

// operationHeader is how a caller names the operation it is about to start.
// Minted by the caller because the question can arrive before the response
// does: a server-generated id would only be known once it was too late.
const operationHeader = "X-Reasonix-Operation-ID"

// Codes a late or misdirected answer is refused with. Four rather than one,
// because a client can act on the difference: a cancelled operation is gone, a
// resolved one already has its answer, and a stale epoch means this reply was
// meant for a kernel that is no longer running.
const (
	codeAskNotFound        = "ask.not_found"
	codeAskStaleEpoch      = "ask.stale_epoch"
	codeAskCancelled       = "ask.cancelled"
	codeAskAlreadyResolved = "ask.already_resolved"
)

var (
	errAskNotFound        = errors.New("no question by that name is open")
	errAskStaleEpoch      = errors.New("that answer belongs to an earlier run of this kernel")
	errAskCancelled       = errors.New("the operation that asked has ended")
	errAskAlreadyResolved = errors.New("that question already has a different answer")
)

// Ask is a question the link layer stopped for. It carries three identities:
// the kernel lifetime it belongs to, the operation it is blocking, and itself.
// The first is what stops an answer landing on a question a restart happened to
// number the same; the second is what tells two dials apart.
type Ask struct {
	Epoch       string `json:"epoch"`
	OperationID string `json:"operationId"`
	ID          string `json:"askId"`
	Kind        string `json:"kind"`
	Host        string `json:"host"`
	Address     string `json:"address,omitempty"`
	KeyType     string `json:"keyType,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	// Set for a locked key: which file it is.
	IdentityFile string    `json:"identityFile,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// AskAnswer is what a person said. Text is the secret where one was asked for,
// and empty everywhere else.
type AskAnswer struct {
	OK   bool   `json:"ok"`
	Text string `json:"text,omitempty"`
}

// settled is what a question became, kept after the fact so a retry can be told
// from a second opinion. There are never many, and they last one launch.
type settled struct {
	cancelled bool
	answer    AskAnswer
}

type live struct {
	ask   Ask
	reply chan AskAnswer
}

// AskBroker holds the questions this launch is blocked on. It is the one place
// they exist: a shell may be told about one, but it never owns it.
type AskBroker struct {
	epoch string
	// notify wakes a client that would otherwise only find out by asking. It is
	// a convenience over the snapshot, never the record.
	notify func(Ask)

	mu   sync.Mutex
	open map[string]*live
	past map[string]settled
}

func NewAskBroker(notify func(Ask)) *AskBroker {
	return &AskBroker{epoch: newAskID("epoch"), notify: notify, open: map[string]*live{}, past: map[string]settled{}}
}

// Epoch names this kernel lifetime.
func (b *AskBroker) Epoch() string { return b.epoch }

func newAskID(prefix string) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail on a working system, and a predictable id
		// here would let a late answer land on a question it was not meant for.
		panic("serve: crypto/rand failed: " + err.Error())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

// Ask raises a question and blocks until it is answered, the operation ends, or
// patience runs out. The last two settle it as cancelled, so a reply that
// arrives afterwards is told what became of it rather than dropped.
func (b *AskBroker) Ask(ctx context.Context, q Ask) (AskAnswer, error) {
	q.Epoch = b.epoch
	q.ID = newAskID("ask")
	q.CreatedAt = time.Now().UTC()
	if q.OperationID = OperationIDFrom(ctx); q.OperationID == "" {
		// Nothing named the operation, so the question still exists and can
		// still be found — just not by a caller narrowing to its own.
		q.OperationID = newAskID("op")
	}
	reply := make(chan AskAnswer, 1)
	b.mu.Lock()
	b.open[q.ID] = &live{ask: q, reply: reply}
	b.mu.Unlock()

	if b.notify != nil {
		b.notify(q)
	}
	select {
	case answer := <-reply:
		return answer, nil
	case <-ctx.Done():
		b.cancel(q.ID)
		return AskAnswer{}, ctx.Err()
	case <-time.After(askPatience):
		b.cancel(q.ID)
		return AskAnswer{}, fmt.Errorf("remote: nobody answered %s", q.Kind)
	}
}

func (b *AskBroker) cancel(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.open[id]; !ok {
		return
	}
	delete(b.open, id)
	b.past[id] = settled{cancelled: true}
}

// Pending is what is open right now, narrowed to one operation when asked. A
// caller polling its own dial must not be shown another's question.
func (b *AskBroker) Pending(operationID string) []Ask {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []Ask{}
	for _, held := range b.open {
		if operationID == "" || held.ask.OperationID == operationID {
			out = append(out, held.ask)
		}
	}
	return out
}

// Answer settles a question. Repeating the same answer succeeds: a POST whose
// response was lost has to be safe to send again, and the client cannot know
// whether the first one landed. A different answer to a settled question is a
// second opinion, and is refused.
func (b *AskBroker) Answer(epoch, operationID, askID string, answer AskAnswer) error {
	if epoch != "" && epoch != b.epoch {
		return errAskStaleEpoch
	}
	b.mu.Lock()
	held, open := b.open[askID]
	if open && operationID != "" && held.ask.OperationID != operationID {
		b.mu.Unlock()
		return errAskNotFound
	}
	if open {
		delete(b.open, askID)
		b.past[askID] = settled{answer: answer}
		b.mu.Unlock()
		held.reply <- answer
		return nil
	}
	before, known := b.past[askID]
	b.mu.Unlock()
	switch {
	case !known:
		return errAskNotFound
	case before.cancelled:
		return errAskCancelled
	case before.answer != answer:
		return errAskAlreadyResolved
	default:
		return nil
	}
}

type operationKey struct{}

// WithOperationID names the operation a request is about to run, so a question
// raised deep inside it can say which dial it is blocking.
func WithOperationID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, operationKey{}, id)
}

func OperationIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(operationKey{}).(string)
	return id
}

// operationContext is what every remote entry point runs its dial under: the
// request's own lifetime, named by whatever the caller called this operation.
func operationContext(r *http.Request) context.Context {
	return WithOperationID(r.Context(), r.Header.Get(operationHeader))
}

func (h *Hub) registerAskRoutes(mux *http.ServeMux) {
	if h.opts.Asks == nil {
		return
	}
	mux.HandleFunc("GET /asks", h.listAsks)
	mux.HandleFunc("POST /asks/{id}/answer", h.answerAsk)
}

func (h *Hub) listAsks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, struct {
		Epoch string `json:"epoch"`
		Asks  []Ask  `json:"asks"`
	}{h.opts.Asks.Epoch(), h.opts.Asks.Pending(r.URL.Query().Get("operationId"))})
}

func (h *Hub) answerAsk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Epoch       string    `json:"epoch"`
		OperationID string    `json:"operationId"`
		Answer      AskAnswer `json:"answer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	switch err := h.opts.Asks.Answer(body.Epoch, body.OperationID, r.PathValue("id"), body.Answer); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, errAskStaleEpoch):
		refuse(w, http.StatusConflict, codeAskStaleEpoch, err.Error(), nil)
	case errors.Is(err, errAskCancelled):
		refuse(w, http.StatusConflict, codeAskCancelled, err.Error(), nil)
	case errors.Is(err, errAskAlreadyResolved):
		refuse(w, http.StatusConflict, codeAskAlreadyResolved, err.Error(), nil)
	default:
		refuse(w, http.StatusNotFound, codeAskNotFound, err.Error(), nil)
	}
}
