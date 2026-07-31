/*
 * MIT License
 *
 * Copyright (c) 2022 Lark Technologies Pte. Ltd.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice, shall be included in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package larkim

import "context"

// protocol v1 的 message_read
type P1MessageReadV1Handler struct {
	handler func(context.Context, *P1MessageReadV1) error
}

func NewP1MessageReadV1Handler(handler func(context.Context, *P1MessageReadV1) error) *P1MessageReadV1Handler {
	h := &P1MessageReadV1Handler{handler: handler}
	return h
}

func (h *P1MessageReadV1Handler) Event() interface{} {
	return &P1MessageReadV1{}
}

func (h *P1MessageReadV1Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1MessageReadV1))
}

// protocol v1 的 message
type P1MessageReceiveV1Handler struct {
	handler func(context.Context, *P1MessageReceiveV1) error
}

func NewP1MessageReceiveV1Handler(handler func(context.Context, *P1MessageReceiveV1) error) *P1MessageReceiveV1Handler {
	h := &P1MessageReceiveV1Handler{handler: handler}
	return h
}

func (h *P1MessageReceiveV1Handler) Event() interface{} {
	return &P1MessageReceiveV1{}
}

func (h *P1MessageReceiveV1Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1MessageReceiveV1))
}

type P1AddBotV1Handler struct {
	handler func(context.Context, *P1AddBotV1) error
}

func NewP1AddBotV1Handler(handler func(context.Context, *P1AddBotV1) error) *P1AddBotV1Handler {
	h := &P1AddBotV1Handler{handler: handler}
	return h
}

func (h *P1AddBotV1Handler) Event() interface{} {
	return &P1AddBotV1{}
}

func (h *P1AddBotV1Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1AddBotV1))
}

type P1RemoveBotV1Handler struct {
	handler func(context.Context, *P1RemoveBotV1) error
}

func NewP1RemoveBotV1Handler(handler func(context.Context, *P1RemoveBotV1) error) *P1RemoveBotV1Handler {
	h := &P1RemoveBotV1Handler{handler: handler}
	return h
}

func (h *P1RemoveBotV1Handler) Event() interface{} {
	return &P1RemoveBotV1{}
}

func (h *P1RemoveBotV1Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1RemoveBotV1))
}

type P1UserInOutChatV1Handler struct {
	handler func(context.Context, *P1UserInOutChatV1) error
}

func NewP1UserInOutChatV1Handler(handler func(context.Context, *P1UserInOutChatV1) error) *P1UserInOutChatV1Handler {
	h := &P1UserInOutChatV1Handler{handler: handler}
	return h
}

func (h *P1UserInOutChatV1Handler) Event() interface{} {
	return &P1UserInOutChatV1{}
}

func (h *P1UserInOutChatV1Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1UserInOutChatV1))
}

type P1ChatDisbandV1Handler struct {
	handler func(context.Context, *P1ChatDisbandV1) error
}

func NewP1DisbandChatV1Handler(handler func(context.Context, *P1ChatDisbandV1) error) *P1ChatDisbandV1Handler {
	h := &P1ChatDisbandV1Handler{handler: handler}
	return h
}

func (h *P1ChatDisbandV1Handler) Event() interface{} {
	return &P1ChatDisbandV1{}
}

func (h *P1ChatDisbandV1Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1ChatDisbandV1))
}

type P1GroupSettingUpdatedV1Handler struct {
	handler func(context.Context, *P1GroupSettingUpdatedV1) error
}

func NewP1GroupSettingUpdatedV1Handler(handler func(context.Context, *P1GroupSettingUpdatedV1) error) *P1GroupSettingUpdatedV1Handler {
	h := &P1GroupSettingUpdatedV1Handler{handler: handler}
	return h
}

func (h *P1GroupSettingUpdatedV1Handler) Event() interface{} {
	return &P1GroupSettingUpdatedV1{}
}

func (h *P1GroupSettingUpdatedV1Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1GroupSettingUpdatedV1))
}

type P1P2PChatCreatedV1Handler struct {
	handler func(context.Context, *P1P2PChatCreatedV1) error
}

func NewP1P2PChatCreatedV1Handler(handler func(context.Context, *P1P2PChatCreatedV1) error) *P1P2PChatCreatedV1Handler {
	h := &P1P2PChatCreatedV1Handler{handler: handler}
	return h
}

func (h *P1P2PChatCreatedV1Handler) Event() interface{} {
	return &P1P2PChatCreatedV1{}
}

func (h *P1P2PChatCreatedV1Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1P2PChatCreatedV1))
}
