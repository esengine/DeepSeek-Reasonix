package hostrpc

import (
	"context"
	"errors"
	"testing"
)

var cancelWhileDecoding context.CancelFunc

type cancelingArgument string
type requestContextKey struct{}

func (*cancelingArgument) UnmarshalJSON([]byte) error { cancelWhileDecoding(); return nil }

type cancellationTarget struct {
	calls  int
	cancel context.CancelFunc
	seen   context.Context
}

func (f *cancellationTarget) DecodeWrite(cancelingArgument) { f.calls++ }
func (f *cancellationTarget) Write() string                 { f.calls++; f.cancel(); return "committed" }
func (f *cancellationTarget) Cooperative(ctx context.Context, value string) (string, error) {
	f.seen = ctx
	f.calls++
	return value, nil
}

func TestCancellationAfterDecodeDoesNotDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelWhileDecoding = cancel
	t.Cleanup(func() { cancelWhileDecoding = nil })
	target := &cancellationTarget{}
	_, err := mustRegistry(t, target, nil).Invoke(ctx, "DecodeWrite", raw(`"value"`))
	if !errors.Is(err, context.Canceled) || target.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, target.calls)
	}
}

func TestDispatchedWriteKeepsSuccessAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	target := &cancellationTarget{cancel: cancel}
	got, err := mustRegistry(t, target, nil).Invoke(ctx, "Write", nil)
	if err != nil || got != "committed" || target.calls != 1 || ctx.Err() == nil {
		t.Fatalf("result=%v err=%v calls=%d", got, err, target.calls)
	}
}

func TestLeadingContextIsHostOnlyAndPassedToOwner(t *testing.T) {
	target := &cancellationTarget{}
	r := mustRegistry(t, target, nil)
	ctx := context.WithValue(context.Background(), requestContextKey{}, "request")
	got, err := r.Invoke(ctx, "Cooperative", raw(`"value"`))
	if err != nil || got != "value" || target.seen != ctx {
		t.Fatalf("result=%v err=%v context=%v", got, err, target.seen)
	}
	for _, cmd := range r.Commands() {
		if cmd.Name == "Cooperative" && (len(cmd.Params) != 1 || cmd.Params[0].Kind != KindString || cmd.Cancellation != "cooperative-context") {
			t.Fatalf("wire command=%+v", cmd)
		}
	}
}
