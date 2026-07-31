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

package larkapplication

import "context"

type P1OrderPaidV6Handler struct {
	handler func(context.Context, *P1OrderPaidV6) error
}

func NewP1OrderPaidV6Handler(handler func(context.Context, *P1OrderPaidV6) error) *P1OrderPaidV6Handler {
	h := &P1OrderPaidV6Handler{handler: handler}
	return h
}

func (h *P1OrderPaidV6Handler) Event() interface{} {
	return &P1OrderPaidV6{}
}

func (h *P1OrderPaidV6Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1OrderPaidV6))
}

type P1AppUninstalledV6Handler struct {
	handler func(context.Context, *P1AppUninstalledV6) error
}

func NewP1AppUninstalledV6Handler(handler func(context.Context, *P1AppUninstalledV6) error) *P1AppUninstalledV6Handler {
	h := &P1AppUninstalledV6Handler{handler: handler}
	return h
}

func (h *P1AppUninstalledV6Handler) Event() interface{} {
	return &P1AppUninstalledV6{}
}

func (h *P1AppUninstalledV6Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1AppUninstalledV6))
}

type P1AppStatusChangedV6Handler struct {
	handler func(context.Context, *P1AppStatusChangedV6) error
}

func NewP1AppStatusChangedV6Handler(handler func(context.Context, *P1AppStatusChangedV6) error) *P1AppStatusChangedV6Handler {
	h := &P1AppStatusChangedV6Handler{handler: handler}
	return h
}

func (h *P1AppStatusChangedV6Handler) Event() interface{} {
	return &P1AppStatusChangedV6{}
}

func (h *P1AppStatusChangedV6Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1AppStatusChangedV6))
}

type P1AppOpenV6Handler struct {
	handler func(context.Context, *P1AppOpenV6) error
}

func NewP1AppOpenV6Handler(handler func(context.Context, *P1AppOpenV6) error) *P1AppOpenV6Handler {
	h := &P1AppOpenV6Handler{handler: handler}
	return h
}

func (h *P1AppOpenV6Handler) Event() interface{} {
	return &P1AppOpenV6{}
}

func (h *P1AppOpenV6Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1AppOpenV6))
}
