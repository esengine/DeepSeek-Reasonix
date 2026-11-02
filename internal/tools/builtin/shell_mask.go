package builtin

import (
	"context"
	"encoding/json"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
)

type progressWriter func(string)

func (w progressWriter) Write(p []byte) (int, error) {
	w(string(p))
	return len(p), nil
}

// maskedProgress masks what a shell tool streams to the window, holding back a
// fragment that could begin a value until the rest arrives; flush passes on
// what is left once the tool returns.
func maskedProgress(ctx context.Context) (context.Context, func()) {
	emit, ok := tool.ProgressFrom(ctx)
	if !ok {
		return ctx, func() {}
	}
	s := secrets.NewStream(progressWriter(emit))
	return tool.WithProgress(ctx, func(chunk string) { _, _ = s.Write([]byte(chunk)) }), func() { _ = s.Flush() }
}

// maskedShell runs a tool that reads a shell's output with what it streams and
// what it returns masked.
func maskedShell(ctx context.Context, args json.RawMessage, run func(context.Context, json.RawMessage) (string, error)) (string, error) {
	ctx, flush := maskedProgress(ctx)
	out, err := run(ctx, args)
	flush()
	return secrets.Masker().String(out), err
}
