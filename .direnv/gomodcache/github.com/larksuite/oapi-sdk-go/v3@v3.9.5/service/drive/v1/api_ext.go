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

package larkdrive

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/larksuite/oapi-sdk-go/v3/core"
)

func (f *file) ListByIterator(ctx context.Context, req *ListFileReq, options ...larkcore.RequestOptionFunc) (*ListFileIterator, error) {
	return &ListFileIterator{
		ctx:      ctx,
		req:      req,
		listFunc: f.List,
		options:  options,
		limit:    math.MaxInt64}, nil
}

type ListFileIterator struct {
	nextPageToken *string
	items         []*File
	index         int
	limit         int
	ctx           context.Context
	req           *ListFileReq
	listFunc      func(ctx context.Context, req *ListFileReq, options ...larkcore.RequestOptionFunc) (*ListFileResp, error)
	options       []larkcore.RequestOptionFunc
	curlNum       int
}

func (iterator *ListFileIterator) Next() (bool, *File, error) {
	// 达到最大量，则返回
	if iterator.limit > 0 && iterator.curlNum >= iterator.limit {
		return false, nil, nil
	}

	// 为0则拉取数据
	if iterator.index == 0 || iterator.index >= len(iterator.items) {
		if iterator.index != 0 && iterator.nextPageToken == nil {
			return false, nil, nil
		}
		if iterator.nextPageToken != nil {
			iterator.req.apiReq.QueryParams.Set("page_token", *iterator.nextPageToken)
		}
		resp, err := iterator.listFunc(iterator.ctx, iterator.req, iterator.options...)
		if err != nil {
			return false, nil, err
		}

		if resp.Code != 0 {
			return false, nil, errors.New(fmt.Sprintf("Code:%d,Msg:%s", resp.Code, resp.Msg))
		}

		if len(resp.Data.Files) == 0 {
			return false, nil, nil
		}

		iterator.nextPageToken = resp.Data.NextPageToken
		iterator.items = resp.Data.Files
		iterator.index = 0
	}

	block := iterator.items[iterator.index]
	iterator.index++
	iterator.curlNum++
	return true, block, nil
}

func (iterator *ListFileIterator) NextPageToken() *string {
	return iterator.nextPageToken
}
