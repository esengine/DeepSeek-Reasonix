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

package larkcore

import (
	"context"
	"testing"
	"time"
)

func TestCache(t *testing.T) {

	cache := localCache{}
	err := cache.Set(context.Background(), "key1", "value1", time.Second)
	if err != nil {
		t.Errorf("set key failed ,%v", err)
	}

	token, err := cache.Get(context.Background(), "key1")
	if err != nil {
		t.Errorf("get key failed ,%v", err)
	}
	if token == "" {
		t.Errorf("get key empty ,%v", err)

	}
}

//
//func TestCacheTimeout(t *testing.T) {
//
//	cache := localCache{}
//	err := cache.Set(context.Background(), "key1", "value1", time.Second)
//	if err != nil {
//		t.Errorf("set key failed ,%v", err)
//	}
//
//	time.Sleep(2 * time.Second)
//
//	token, err := cache.Get(context.Background(), "key1")
//	if err != nil {
//		t.Errorf("get key failed ,%v", err)
//	}
//	if token == "" {
//		t.Errorf("get key empty ,%v", err)
//
//	}
//}
