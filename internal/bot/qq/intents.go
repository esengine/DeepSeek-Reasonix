package qq

import (
	"context"
	"errors"
	"fmt"
)

// QQ rejects Identify when it contains an intent the bot is not authorized
// for. Try the private-guild profile first to preserve MESSAGE_CREATE support,
// then remember a public-guild fallback for this adapter lifetime if rejected.
const (
	intentGuilds               = 1 << 0
	intentGuildMembers         = 1 << 1
	intentPrivateGuildMessages = 1 << 9
	intentDirectMessage        = 1 << 12
	intentGroupAndC2C          = 1 << 25
	intentPublicGuildMessages  = 1 << 30

	qqSharedIdentifyIntents  = intentGuilds | intentGuildMembers | intentDirectMessage | intentGroupAndC2C
	qqPrivateIdentifyIntents = qqSharedIdentifyIntents | intentPrivateGuildMessages
	qqPublicIdentifyIntents  = qqSharedIdentifyIntents | intentPublicGuildMessages
)

var errQQIdentifyRejected = errors.New("qq gateway identify rejected")

func connectQQGatewayWithIntentFallback(ctx context.Context, token string, selected *int, connect func(context.Context, string, int) error, onFallback func()) (bool, error) {
	err := connect(ctx, token, *selected)
	if *selected != qqPrivateIdentifyIntents || !errors.Is(err, errQQIdentifyRejected) {
		return false, err
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	*selected = qqPublicIdentifyIntents
	if onFallback != nil {
		onFallback()
	}
	return true, connect(ctx, token, *selected)
}

func validateQQReadyPayload(msg gatewayPayload) error {
	if msg.Op == opInvalid {
		return fmt.Errorf("%w: op=%d", errQQIdentifyRejected, msg.Op)
	}
	if msg.Op != opDispatch || msg.T != "READY" {
		return fmt.Errorf("expected op=%d READY, got op=%d event=%q", opDispatch, msg.Op, msg.T)
	}
	return nil
}
