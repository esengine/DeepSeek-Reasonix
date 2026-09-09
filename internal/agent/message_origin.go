package agent

import (
	"context"

	"reasonix/internal/provider"
)

type inputMessageOriginKey struct{}

// WithInputMessageOrigin marks the user-role message Agent.Run will persist.
// Controllers use host origin only for synthetic continuation runs.
func WithInputMessageOrigin(ctx context.Context, origin provider.MessageOrigin) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, inputMessageOriginKey{}, origin)
}

func inputMessageOrigin(ctx context.Context) provider.MessageOrigin {
	if ctx != nil {
		if origin, ok := ctx.Value(inputMessageOriginKey{}).(provider.MessageOrigin); ok && origin == provider.MessageOriginHost {
			return origin
		}
	}
	return provider.MessageOriginUser
}

// HostGeneratedUserMessage constructs a provider-visible user-role protocol
// message whose durable provenance prevents it from being mistaken for user
// intent by transcripts or completion evidence.
func HostGeneratedUserMessage(content string) provider.Message {
	return provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginHost, Content: content}
}
