package dispatcher

import (
	"context"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (dispatcher *EventDispatcher) OnP2CardActionTrigger(handler func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error)) *EventDispatcher {
	_, existed := dispatcher.callbackType2CallbackHandler["card.action.trigger"]
	if existed {
		panic("event: multiple handler registrations for " + "card.action.trigger")
	}
	dispatcher.callbackType2CallbackHandler["card.action.trigger"] = callback.NewCardActionTriggerEventHandler(handler)
	return dispatcher
}

func (dispatcher *EventDispatcher) OnP2CardURLPreviewGet(handler func(ctx context.Context, event *callback.URLPreviewGetEvent) (*callback.URLPreviewGetResponse, error)) *EventDispatcher {
	_, existed := dispatcher.callbackType2CallbackHandler["url.preview.get"]
	if existed {
		panic("event: multiple handler registrations for " + "url.preview.get")
	}
	dispatcher.callbackType2CallbackHandler["url.preview.get"] = callback.NewURLPreviewGetEventHandler(handler)
	return dispatcher
}
