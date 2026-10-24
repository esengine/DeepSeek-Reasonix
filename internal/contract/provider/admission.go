package provider

import (
	"context"
	"errors"
)

// RequestAdmission reserves execution capacity for one physical model request.
// The release callback must be safe to call once after the source stream closes.
type RequestAdmission interface {
	AcquireRequest(context.Context) (release func(), err error)
}

type requestAdmissionKey struct{}
type requestAdmissionValue struct{ admission RequestAdmission }

func WithRequestAdmission(ctx context.Context, admission RequestAdmission) context.Context {
	return context.WithValue(ctx, requestAdmissionKey{}, requestAdmissionValue{admission})
}

// Stream applies host admission without wrapping the provider or its traits.
// Cancellation closes the consumer stream, but retains capacity until the
// provider stream closes; a cancelled caller is not proof of physical completion.
func Stream(ctx context.Context, p Provider, req Request) (<-chan Chunk, error) {
	value, _ := ctx.Value(requestAdmissionKey{}).(requestAdmissionValue)
	admission := value.admission
	if admission == nil {
		return p.Stream(ctx, req)
	}
	release, err := admission.AcquireRequest(ctx)
	if err != nil {
		return nil, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			release()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input, err := p.Stream(WithRequestAdmission(ctx, nil), req)
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, errors.New("provider returned a nil stream without an error")
	}
	output := make(chan Chunk)
	handedOff = true
	go forwardAdmittedStream(ctx, input, output, release)
	return output, nil
}

func forwardAdmittedStream(ctx context.Context, input <-chan Chunk, output chan<- Chunk, release func()) {
	defer release()
	for {
		select {
		case <-ctx.Done():
			close(output)
			for range input {
			}
			return
		case chunk, ok := <-input:
			if !ok {
				close(output)
				return
			}
			select {
			case output <- chunk:
			case <-ctx.Done():
				close(output)
				for range input {
				}
				return
			}
		}
	}
}
