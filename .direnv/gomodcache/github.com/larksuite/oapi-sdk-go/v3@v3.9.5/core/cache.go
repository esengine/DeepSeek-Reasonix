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
	"sync"
	"time"
)

var cache = &localCache{}

func NewCache(config *Config) {
	if config.TokenCache != nil {
		tokenManager = TokenManager{cache: config.TokenCache}
		appTicketManager = AppTicketManager{cache: config.TokenCache}
	}
}

type Cache interface {
	Set(ctx context.Context, key string, value string, expireTime time.Duration) error
	Get(ctx context.Context, key string) (string, error)
}

type localCache struct {
	m sync.Map
}

func (s *localCache) Get(ctx context.Context, key string) (string, error) {
	if val, ok := s.m.Load(key); ok {
		ev := val.(*Value)
		//fmt.Println(fmt.Sprintf("get key:%s,hit cache,time left %f seconds",
		//	key, ev.expireTime.Sub(time.Now()).Seconds()))
		if ev.expireTime.After(time.Now()) {
			return ev.value, nil
		}
	}
	return "", nil
}

func (s *localCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	expireTime := time.Now().Add(ttl)
	s.m.Store(key, &Value{
		value:      value,
		expireTime: expireTime,
	})
	return nil
}

type Value struct {
	value      string
	expireTime time.Time
}
