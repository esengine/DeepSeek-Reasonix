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

package dispatcher

import (
	"context"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event"
)

type appTicketEventData struct {
	AppId     string `json:"app_id"`
	Type      string `json:"type"`
	AppTicket string `json:"app_ticket"`
}

type AppTicketEvent struct {
	*larkevent.EventBase
	Event *appTicketEventData `json:"event"`
}

type appTicketEventHandler struct {
	event *AppTicketEvent
}

func (h *appTicketEventHandler) Event() interface{} {
	h.event = &AppTicketEvent{}
	return h.event
}

func (h *appTicketEventHandler) Handle(ctx context.Context, event interface{}) error {
	appTicketEvent := event.(*AppTicketEvent)
	return larkcore.GetAppTicketManager().Set(context.Background(),
		appTicketEvent.Event.AppId,
		appTicketEvent.Event.AppTicket, time.Hour*12)
}
