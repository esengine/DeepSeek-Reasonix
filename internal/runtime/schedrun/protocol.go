package schedrun

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"reasonix/internal/contract/observe"
	"reasonix/internal/state/schedule"
)

// Line kinds on the child's standard output.
const (
	KindReady  = "ready"
	KindUsage  = "usage"
	KindResult = "result"
)

// MaxLineBytes bounds one line from the child. A result carries a report and up
// to observe.MaxPending records, each cut to its own bound.
const MaxLineBytes = 512 << 10

// Line is one message from the child.
type Line struct {
	Kind   string `json:"kind"`
	Tokens int64  `json:"tokens,omitempty"`
	Result *Done  `json:"result,omitempty"`
}

// Done is the child's account of how the run ended. Tokens and Usages are its
// own totals; the supervisor compares them with what it counted from the stream
// before it believes the usage stream ran to the end.
type Done struct {
	State  schedule.RunState `json:"state"`
	Code   string            `json:"code,omitempty"`
	Tokens int64             `json:"tokens"`
	Usages int64             `json:"usages"`
	// Unmetered is set when the model answered a request with no usage report,
	// so the tokens the run spent are not known.
	Unmetered   bool              `json:"unmetered,omitempty"`
	Report      string            `json:"report,omitempty"`
	Pending     []observe.Pending `json:"pending,omitempty"`
	Posture     observe.Posture   `json:"posture"`
	SessionPath string            `json:"sessionPath,omitempty"`
}

// Writer serialises lines onto the child's standard output. The supervisor's
// reader expects nothing else there, so a child writes through this alone.
type Writer struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func NewWriter(w io.Writer) *Writer { return &Writer{enc: json.NewEncoder(w)} }

func (w *Writer) Ready() error { return w.write(Line{Kind: KindReady}) }

func (w *Writer) Usage(tokens int64) error {
	return w.write(Line{Kind: KindUsage, Tokens: tokens})
}

func (w *Writer) Result(d Done) error {
	return w.write(Line{Kind: KindResult, Result: &d})
}

func (w *Writer) write(l Line) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(l)
}

// readLines feeds fn every well-formed line until the stream ends. A line past
// MaxLineBytes ends the stream with ErrProtocol; a line that is not a Line is
// skipped, because stray output on the stream must not read as a usage report.
func readLines(r io.Reader, fn func(Line)) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if len(buf)+len(chunk) > MaxLineBytes {
			return fmt.Errorf("%w: a line passed %d bytes", ErrProtocol, MaxLineBytes)
		}
		buf = append(buf, chunk...)
		if isPrefix {
			continue
		}
		var l Line
		if json.Unmarshal(buf, &l) == nil && (l.Kind == KindUsage || l.Kind == KindResult || l.Kind == KindReady) {
			fn(l)
		}
		buf = buf[:0]
	}
}
