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

package larkapproval

import "context"

type P1LeaveApprovalV4Handler struct {
	handler func(context.Context, *P1LeaveApprovalV4) error
}

func NewP1LeaveApprovalV4Handler(handler func(context.Context, *P1LeaveApprovalV4) error) *P1LeaveApprovalV4Handler {
	h := &P1LeaveApprovalV4Handler{handler: handler}
	return h
}

func (h *P1LeaveApprovalV4Handler) Event() interface{} {
	return &P1LeaveApprovalV4{}
}

func (h *P1LeaveApprovalV4Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1LeaveApprovalV4))
}

type P1WorkApprovalV4Handler struct {
	handler func(context.Context, *P1WorkApprovalV4) error
}

func NewP1WorkApprovalV4Handler(handler func(context.Context, *P1WorkApprovalV4) error) *P1WorkApprovalV4Handler {
	h := &P1WorkApprovalV4Handler{handler: handler}
	return h
}

func (h *P1WorkApprovalV4Handler) Event() interface{} {
	return &P1WorkApprovalV4{}
}

func (h *P1WorkApprovalV4Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1WorkApprovalV4))
}

type P1ShiftApprovalV4Handler struct {
	handler func(context.Context, *P1ShiftApprovalV4) error
}

func NewP1ShiftApprovalV4Handler(handler func(context.Context, *P1ShiftApprovalV4) error) *P1ShiftApprovalV4Handler {
	h := &P1ShiftApprovalV4Handler{handler: handler}
	return h
}

func (h *P1ShiftApprovalV4Handler) Event() interface{} {
	return &P1ShiftApprovalV4{}
}

func (h *P1ShiftApprovalV4Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1ShiftApprovalV4))
}

type P1RemedyApprovalV4Handler struct {
	handler func(context.Context, *P1RemedyApprovalV4) error
}

func NewP1RemedyApprovalV4Handler(handler func(context.Context, *P1RemedyApprovalV4) error) *P1RemedyApprovalV4Handler {
	h := &P1RemedyApprovalV4Handler{handler: handler}
	return h
}

func (h *P1RemedyApprovalV4Handler) Event() interface{} {
	return &P1RemedyApprovalV4{}
}

func (h *P1RemedyApprovalV4Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1RemedyApprovalV4))
}

type P1TripApprovalV4Handler struct {
	handler func(context.Context, *P1TripApprovalV4) error
}

func NewP1TripApprovalV4Handler(handler func(context.Context, *P1TripApprovalV4) error) *P1TripApprovalV4Handler {
	h := &P1TripApprovalV4Handler{handler: handler}
	return h
}

func (h *P1TripApprovalV4Handler) Event() interface{} {
	return &P1TripApprovalV4{}
}

func (h *P1TripApprovalV4Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1TripApprovalV4))
}

type P1OutApprovalV4Handler struct {
	handler func(context.Context, *P1OutApprovalV4) error
}

func NewP1OutApprovalV4Handler(handler func(context.Context, *P1OutApprovalV4) error) *P1OutApprovalV4Handler {
	h := &P1OutApprovalV4Handler{handler: handler}
	return h
}

func (h *P1OutApprovalV4Handler) Event() interface{} {
	return &P1OutApprovalV4{}
}

func (h *P1OutApprovalV4Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1OutApprovalV4))
}
