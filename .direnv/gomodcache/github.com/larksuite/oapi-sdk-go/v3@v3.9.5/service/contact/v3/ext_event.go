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

package larkcontact

import "context"

type P1UserStatusChangedV3Handler struct {
	handler func(context.Context, *P1UserStatusChangedV3) error
}

func NewP1UserStatusChangedV3Handler(handler func(context.Context, *P1UserStatusChangedV3) error) *P1UserStatusChangedV3Handler {
	h := &P1UserStatusChangedV3Handler{handler: handler}
	return h
}

func (h *P1UserStatusChangedV3Handler) Event() interface{} {
	return &P1UserStatusChangedV3{}
}

func (h *P1UserStatusChangedV3Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1UserStatusChangedV3))
}

type P1UserChangedV3Handler struct {
	handler func(context.Context, *P1UserChangedV3) error
}

func NewP1UserChangedV3Handler(handler func(context.Context, *P1UserChangedV3) error) *P1UserChangedV3Handler {
	h := &P1UserChangedV3Handler{handler: handler}
	return h
}

func (h *P1UserChangedV3Handler) Event() interface{} {
	return &P1UserChangedV3{}
}

func (h *P1UserChangedV3Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1UserChangedV3))
}

type P1DepartmentChangedV3Handler struct {
	handler func(context.Context, *P1DepartmentChangedV3) error
}

func NewP1DepartmentChangedV3Handler(handler func(context.Context, *P1DepartmentChangedV3) error) *P1DepartmentChangedV3Handler {
	h := &P1DepartmentChangedV3Handler{handler: handler}
	return h
}

func (h *P1DepartmentChangedV3Handler) Event() interface{} {
	return &P1DepartmentChangedV3{}
}

func (h *P1DepartmentChangedV3Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1DepartmentChangedV3))
}

type P1ContactScopeChangedV3Handler struct {
	handler func(context.Context, *P1ContactScopeChangedV3) error
}

func NewP1ContactScopeChangedV3Handler(handler func(context.Context, *P1ContactScopeChangedV3) error) *P1ContactScopeChangedV3Handler {
	h := &P1ContactScopeChangedV3Handler{handler: handler}
	return h
}

func (h *P1ContactScopeChangedV3Handler) Event() interface{} {
	return &P1ContactScopeChangedV3{}
}

func (h *P1ContactScopeChangedV3Handler) Handle(ctx context.Context, event interface{}) error {
	return h.handler(ctx, event.(*P1ContactScopeChangedV3))
}
