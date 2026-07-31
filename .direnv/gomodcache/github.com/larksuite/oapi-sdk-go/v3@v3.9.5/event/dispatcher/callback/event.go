package callback

import "context"

// 消息处理器定义
type CardActionTriggerEventHandler struct {
	handler func(context.Context, *CardActionTriggerEvent) (*CardActionTriggerResponse, error)
}

func NewCardActionTriggerEventHandler(handler func(context.Context, *CardActionTriggerEvent) (*CardActionTriggerResponse, error)) *CardActionTriggerEventHandler {
	h := &CardActionTriggerEventHandler{handler: handler}
	return h
}

// 返回事件的消息体的实例，用于反序列化用
func (h *CardActionTriggerEventHandler) Event() interface{} {
	return &CardActionTriggerEvent{}
}

// 回调开发者注册的handle
func (h *CardActionTriggerEventHandler) Handle(ctx context.Context, event interface{}) (interface{}, error) {
	return h.handler(ctx, event.(*CardActionTriggerEvent))
}

// 消息处理器定义
type URLPreviewGetEventHandler struct {
	handler func(context.Context, *URLPreviewGetEvent) (*URLPreviewGetResponse, error)
}

func NewURLPreviewGetEventHandler(handler func(context.Context, *URLPreviewGetEvent) (*URLPreviewGetResponse, error)) *URLPreviewGetEventHandler {
	h := &URLPreviewGetEventHandler{handler: handler}
	return h
}

// 返回事件的消息体的实例，用于反序列化用
func (h *URLPreviewGetEventHandler) Event() interface{} {
	return &URLPreviewGetEvent{}
}

// 回调开发者注册的handle
func (h *URLPreviewGetEventHandler) Handle(ctx context.Context, event interface{}) (interface{}, error) {
	return h.handler(ctx, event.(*URLPreviewGetEvent))
}
