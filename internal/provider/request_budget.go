package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// ErrRequestBudgetExhausted identifies a provider request rejected before any
// network call because its conservative input estimate cannot fit the active
// host budget.
var ErrRequestBudgetExhausted = errors.New("provider request budget exhausted")

// RequestBudgetError describes a fail-closed request admission rejection.
type RequestBudgetError struct {
	Used           int
	Limit          int
	Remaining      int
	EstimatedInput int
}

func (e *RequestBudgetError) Error() string {
	if e == nil {
		return ErrRequestBudgetExhausted.Error()
	}
	return fmt.Sprintf("%s: %d tokens remain (%d/%d used), but the next request needs about %d input tokens before output",
		ErrRequestBudgetExhausted, e.Remaining, e.Used, e.Limit, e.EstimatedInput)
}

func (e *RequestBudgetError) Unwrap() error { return ErrRequestBudgetExhausted }

// RequestBudgetReservation keeps concurrent provider calls from admitting
// against the same remaining allowance. Commit records exact terminal usage;
// Cancel releases the reservation when no billable usage was observed.
type RequestBudgetReservation interface {
	Commit(tokens int)
	Cancel()
}

// RequestBudget is an optional context capability used by Goal turns. The
// implementation may reduce maxOutputTokens so estimated input plus bounded
// output fits, or reject the request before Provider.Stream is called.
type RequestBudget interface {
	ReserveProviderRequest(estimatedInput, maxOutputTokens int) (adjustedMaxOutput int, reservation RequestBudgetReservation, err error)
}

type requestBudgetContextKey struct{}

// WithRequestBudget attaches a host-local request gate. It never enters a
// provider request or prompt-cache prefix.
func WithRequestBudget(ctx context.Context, budget RequestBudget) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if budget == nil {
		return ctx
	}
	return context.WithValue(ctx, requestBudgetContextKey{}, budget)
}

func requestBudgetFromContext(ctx context.Context) RequestBudget {
	if ctx == nil {
		return nil
	}
	budget, _ := ctx.Value(requestBudgetContextKey{}).(RequestBudget)
	return budget
}

// StreamWithRequestBudget is the single request-admission boundary for
// billable runtime calls. With no Goal budget it is a zero-behavior-change
// pass-through. With a budget it reserves before the network call, commits the
// provider's exact usage before closing the returned stream, and marks that
// usage so event accounting cannot double count it.
func StreamWithRequestBudget(ctx context.Context, p Provider, req Request) (<-chan Chunk, error) {
	return StreamWithRequestBudgetEstimate(ctx, p, req, 0)
}

// StreamWithRequestBudgetEstimate is the field-local variant used by agents
// that have an exact prompt-token floor from the preceding append-only round.
// The estimate never enters Request, so exact replay and provider-visible
// request equality remain unchanged.
func StreamWithRequestBudgetEstimate(ctx context.Context, p Provider, req Request, inputFloor int) (<-chan Chunk, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	budget := requestBudgetFromContext(ctx)
	if budget == nil {
		return p.Stream(ctx, req)
	}
	estimatedInput := EstimateRequestInputTokens(req)
	if inputFloor > estimatedInput {
		estimatedInput = inputFloor
	}
	maxOutput, reservation, err := budget.ReserveProviderRequest(estimatedInput, req.MaxTokens)
	if err != nil {
		return nil, err
	}
	req.MaxTokens = maxOutput
	ch, err := p.Stream(ctx, req)
	if err != nil {
		reservation.Cancel()
		return nil, err
	}
	out := make(chan Chunk, 16)
	go func() {
		defer close(out)
		var usage *Usage
		defer func() {
			if usage == nil {
				reservation.Cancel()
				return
			}
			reservation.Commit(usageTotalTokens(usage))
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-ch:
				if !ok {
					return
				}
				if chunk.Type == ChunkUsage && chunk.Usage != nil {
					cloned := *chunk.Usage
					cloned.BudgetAccounted = true
					chunk.Usage = &cloned
					usage = &cloned
				}
				select {
				case out <- chunk:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// EstimateRequestInputTokens conservatively estimates provider-visible input.
// It handles CJK as roughly one token per rune, code/JSON as roughly four bytes
// per token, and includes chat/tool framing.
func EstimateRequestInputTokens(req Request) int {
	total := 3
	for _, msg := range ModelMessages(req.Messages) {
		total += 4
		total += estimateBudgetTextTokens(msg.Content)
		total += estimateBudgetTextTokens(msg.ReasoningContent)
		total += estimateBudgetTextTokens(msg.ReasoningSignature)
		total += estimateBudgetTextTokens(msg.Name)
		total += estimateBudgetTextTokens(msg.ToolCallID)
		for _, image := range msg.Images {
			total += estimateBudgetTextTokens(image)
		}
		for _, call := range msg.ToolCalls {
			total += 8 + estimateBudgetTextTokens(call.ID) + estimateBudgetTextTokens(call.Name) + estimateBudgetTextTokens(call.Arguments)
		}
		for _, item := range msg.ResponsesItems {
			total += estimateBudgetTextTokens(string(item))
		}
	}
	for _, schema := range req.Tools {
		encoded, _ := json.Marshal(schema)
		total += 8 + estimateBudgetTextTokens(string(encoded))
	}
	if total < 1 {
		return 1
	}
	return total
}

func estimateBudgetTextTokens(text string) int {
	if text == "" {
		return 0
	}
	byBytes := (len(text) + 3) / 4
	if runes := utf8.RuneCountInString(text); runes > byBytes {
		return runes
	}
	return byBytes
}

func usageTotalTokens(usage *Usage) int {
	if usage == nil {
		return 0
	}
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.PromptTokens + usage.CompletionTokens
}
