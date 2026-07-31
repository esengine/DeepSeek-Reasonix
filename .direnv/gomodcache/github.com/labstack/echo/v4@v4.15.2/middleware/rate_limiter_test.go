// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestRateLimiter(t *testing.T) {
	e := echo.New()

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	var inMemoryStore = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 1, Burst: 3})

	mw := RateLimiter(inMemoryStore)

	testCases := []struct {
		id   string
		code int
	}{
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1", http.StatusTooManyRequests},
		{"127.0.0.1", http.StatusTooManyRequests},
		{"127.0.0.1", http.StatusTooManyRequests},
		{"127.0.0.1", http.StatusTooManyRequests},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Add(echo.HeaderXRealIP, tc.id)

		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		_ = mw(handler)(c)
		assert.Equal(t, tc.code, rec.Code)
	}
}

func TestRateLimiter_panicBehaviour(t *testing.T) {
	var inMemoryStore = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 1, Burst: 3})

	assert.Panics(t, func() {
		RateLimiter(nil)
	})

	assert.NotPanics(t, func() {
		RateLimiter(inMemoryStore)
	})
}

func TestRateLimiterWithConfig(t *testing.T) {
	var inMemoryStore = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 1, Burst: 3})

	e := echo.New()

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	mw := RateLimiterWithConfig(RateLimiterConfig{
		IdentifierExtractor: func(c echo.Context) (string, error) {
			id := c.Request().Header.Get(echo.HeaderXRealIP)
			if id == "" {
				return "", errors.New("invalid identifier")
			}
			return id, nil
		},
		DenyHandler: func(ctx echo.Context, identifier string, err error) error {
			return ctx.JSON(http.StatusForbidden, nil)
		},
		ErrorHandler: func(ctx echo.Context, err error) error {
			return ctx.JSON(http.StatusBadRequest, nil)
		},
		Store: inMemoryStore,
	})

	testCases := []struct {
		id   string
		code int
	}{
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1", http.StatusForbidden},
		{"", http.StatusBadRequest},
		{"127.0.0.1", http.StatusForbidden},
		{"127.0.0.1", http.StatusForbidden},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Add(echo.HeaderXRealIP, tc.id)

		rec := httptest.NewRecorder()

		c := e.NewContext(req, rec)

		_ = mw(handler)(c)

		assert.Equal(t, tc.code, rec.Code)
	}
}

func TestRateLimiterWithConfig_defaultDenyHandler(t *testing.T) {
	var inMemoryStore = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 1, Burst: 3})

	e := echo.New()

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	mw := RateLimiterWithConfig(RateLimiterConfig{
		IdentifierExtractor: func(c echo.Context) (string, error) {
			id := c.Request().Header.Get(echo.HeaderXRealIP)
			if id == "" {
				return "", errors.New("invalid identifier")
			}
			return id, nil
		},
		Store: inMemoryStore,
	})

	testCases := []struct {
		id   string
		code int
	}{
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1", http.StatusTooManyRequests},
		{"", http.StatusForbidden},
		{"127.0.0.1", http.StatusTooManyRequests},
		{"127.0.0.1", http.StatusTooManyRequests},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Add(echo.HeaderXRealIP, tc.id)

		rec := httptest.NewRecorder()

		c := e.NewContext(req, rec)

		_ = mw(handler)(c)

		assert.Equal(t, tc.code, rec.Code)
	}
}

func TestRateLimiterWithConfig_defaultConfig(t *testing.T) {
	{
		var inMemoryStore = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 1, Burst: 3})

		e := echo.New()

		handler := func(c echo.Context) error {
			return c.String(http.StatusOK, "test")
		}

		mw := RateLimiterWithConfig(RateLimiterConfig{
			Store: inMemoryStore,
		})

		testCases := []struct {
			id   string
			code int
		}{
			{"127.0.0.1", http.StatusOK},
			{"127.0.0.1", http.StatusOK},
			{"127.0.0.1", http.StatusOK},
			{"127.0.0.1", http.StatusTooManyRequests},
			{"127.0.0.1", http.StatusTooManyRequests},
			{"127.0.0.1", http.StatusTooManyRequests},
			{"127.0.0.1", http.StatusTooManyRequests},
		}

		for _, tc := range testCases {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Add(echo.HeaderXRealIP, tc.id)

			rec := httptest.NewRecorder()

			c := e.NewContext(req, rec)

			_ = mw(handler)(c)

			assert.Equal(t, tc.code, rec.Code)
		}
	}
}

func TestRateLimiterWithConfig_skipper(t *testing.T) {
	e := echo.New()

	var beforeFuncRan bool
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}
	var inMemoryStore = NewRateLimiterMemoryStore(5)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add(echo.HeaderXRealIP, "127.0.0.1")

	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec)

	mw := RateLimiterWithConfig(RateLimiterConfig{
		Skipper: func(c echo.Context) bool {
			return true
		},
		BeforeFunc: func(c echo.Context) {
			beforeFuncRan = true
		},
		Store: inMemoryStore,
		IdentifierExtractor: func(ctx echo.Context) (string, error) {
			return "127.0.0.1", nil
		},
	})

	_ = mw(handler)(c)

	assert.Equal(t, false, beforeFuncRan)
}

func TestRateLimiterWithConfig_skipperNoSkip(t *testing.T) {
	e := echo.New()

	var beforeFuncRan bool
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}
	var inMemoryStore = NewRateLimiterMemoryStore(5)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add(echo.HeaderXRealIP, "127.0.0.1")

	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec)

	mw := RateLimiterWithConfig(RateLimiterConfig{
		Skipper: func(c echo.Context) bool {
			return false
		},
		BeforeFunc: func(c echo.Context) {
			beforeFuncRan = true
		},
		Store: inMemoryStore,
		IdentifierExtractor: func(ctx echo.Context) (string, error) {
			return "127.0.0.1", nil
		},
	})

	_ = mw(handler)(c)

	assert.Equal(t, true, beforeFuncRan)
}

func TestRateLimiterWithConfig_beforeFunc(t *testing.T) {
	e := echo.New()

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	var beforeRan bool
	var inMemoryStore = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 1, Burst: 3})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add(echo.HeaderXRealIP, "127.0.0.1")

	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec)

	mw := RateLimiterWithConfig(RateLimiterConfig{
		BeforeFunc: func(c echo.Context) {
			beforeRan = true
		},
		Store: inMemoryStore,
		IdentifierExtractor: func(ctx echo.Context) (string, error) {
			return "127.0.0.1", nil
		},
	})

	_ = mw(handler)(c)

	assert.Equal(t, true, beforeRan)
}

func TestRateLimiterMemoryStore_Allow(t *testing.T) {
	var inMemoryStore = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 1, Burst: 3, ExpiresIn: 2 * time.Second})
	testCases := []struct {
		id      string
		allowed bool
	}{
		{"127.0.0.1", true},  // 0 ms
		{"127.0.0.1", true},  // 220 ms burst #2
		{"127.0.0.1", true},  // 440 ms burst #3
		{"127.0.0.1", false}, // 660 ms block
		{"127.0.0.1", false}, // 880 ms block
		{"127.0.0.1", true},  // 1100 ms next second #1
		{"127.0.0.2", true},  // 1320 ms allow other ip
		{"127.0.0.1", false}, // 1540 ms no burst
		{"127.0.0.1", false}, // 1760 ms no burst
		{"127.0.0.1", false}, // 1980 ms no burst
		{"127.0.0.1", true},  // 2200 ms no burst
		{"127.0.0.1", false}, // 2420 ms no burst
		{"127.0.0.1", false}, // 2640 ms no burst
		{"127.0.0.1", false}, // 2860 ms no burst
		{"127.0.0.1", true},  // 3080 ms no burst
		{"127.0.0.1", false}, // 3300 ms no burst
		{"127.0.0.1", false}, // 3520 ms no burst
		{"127.0.0.1", false}, // 3740 ms no burst
		{"127.0.0.1", false}, // 3960 ms no burst
		{"127.0.0.1", true},  // 4180 ms no burst
		{"127.0.0.1", false}, // 4400 ms no burst
		{"127.0.0.1", false}, // 4620 ms no burst
		{"127.0.0.1", false}, // 4840 ms no burst
		{"127.0.0.1", true},  // 5060 ms no burst
	}

	for i, tc := range testCases {
		t.Logf("Running testcase #%d => %v", i, time.Duration(i)*220*time.Millisecond)
		inMemoryStore.timeNow = func() time.Time {
			return time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC).Add(time.Duration(i) * 220 * time.Millisecond)
		}
		allowed, _ := inMemoryStore.Allow(tc.id)
		assert.Equal(t, tc.allowed, allowed)
	}
}

func TestRateLimiterMemoryStore_cleanupStaleVisitors(t *testing.T) {
	var inMemoryStore = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 1, Burst: 3})
	inMemoryStore.visitors = map[string]*Visitor{
		"A": {
			Limiter:  rate.NewLimiter(1, 3),
			lastSeen: time.Now(),
		},
		"B": {
			Limiter:  rate.NewLimiter(1, 3),
			lastSeen: time.Now().Add(-1 * time.Minute),
		},
		"C": {
			Limiter:  rate.NewLimiter(1, 3),
			lastSeen: time.Now().Add(-5 * time.Minute),
		},
		"D": {
			Limiter:  rate.NewLimiter(1, 3),
			lastSeen: time.Now().Add(-10 * time.Minute),
		},
	}

	inMemoryStore.Allow("D")
	inMemoryStore.cleanupStaleVisitors()

	var exists bool

	_, exists = inMemoryStore.visitors["A"]
	assert.Equal(t, true, exists)

	_, exists = inMemoryStore.visitors["B"]
	assert.Equal(t, true, exists)

	_, exists = inMemoryStore.visitors["C"]
	assert.Equal(t, false, exists)

	_, exists = inMemoryStore.visitors["D"]
	assert.Equal(t, true, exists)
}

func TestNewRateLimiterMemoryStore(t *testing.T) {
	testCases := []struct {
		rate              rate.Limit
		burst             int
		expiresIn         time.Duration
		expectedExpiresIn time.Duration
	}{
		{1, 3, 5 * time.Second, 5 * time.Second},
		{2, 4, 0, 3 * time.Minute},
		{1, 5, 10 * time.Minute, 10 * time.Minute},
		{3, 7, 0, 3 * time.Minute},
	}

	for _, tc := range testCases {
		store := NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: tc.rate, Burst: tc.burst, ExpiresIn: tc.expiresIn})
		assert.Equal(t, tc.rate, store.rate)
		assert.Equal(t, tc.burst, store.burst)
		assert.Equal(t, tc.expectedExpiresIn, store.expiresIn)
	}
}

func TestRateLimiterMemoryStore_FractionalRateDefaultBurst(t *testing.T) {
	store := NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{
		Rate: 0.5, // fractional rate should get a burst of at least 1
	})

	base := time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
	store.timeNow = func() time.Time {
		return base
	}

	allowed, err := store.Allow("user")
	assert.NoError(t, err)
	assert.True(t, allowed, "first request should not be blocked")

	allowed, err = store.Allow("user")
	assert.NoError(t, err)
	assert.False(t, allowed, "burst token should be consumed immediately")

	store.timeNow = func() time.Time {
		return base.Add(2 * time.Second)
	}

	allowed, err = store.Allow("user")
	assert.NoError(t, err)
	assert.True(t, allowed, "token should refill for fractional rate after time passes")
}

func generateAddressList(count int) []string {
	addrs := make([]string, count)
	for i := 0; i < count; i++ {
		addrs[i] = randomString(15)
	}
	return addrs
}

func run(wg *sync.WaitGroup, store RateLimiterStore, addrs []string, max int, b *testing.B) {
	for i := 0; i < b.N; i++ {
		store.Allow(addrs[rand.Intn(max)])
	}
	wg.Done()
}

func benchmarkStore(store RateLimiterStore, parallel int, max int, b *testing.B) {
	addrs := generateAddressList(max)
	wg := &sync.WaitGroup{}
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go run(wg, store, addrs, max, b)
	}
	wg.Wait()
}

const (
	testExpiresIn = 1000 * time.Millisecond
)

func BenchmarkRateLimiterMemoryStore_1000(b *testing.B) {
	var store = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 100, Burst: 200, ExpiresIn: testExpiresIn})
	benchmarkStore(store, 10, 1000, b)
}

func BenchmarkRateLimiterMemoryStore_10000(b *testing.B) {
	var store = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 100, Burst: 200, ExpiresIn: testExpiresIn})
	benchmarkStore(store, 10, 10000, b)
}

func BenchmarkRateLimiterMemoryStore_100000(b *testing.B) {
	var store = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 100, Burst: 200, ExpiresIn: testExpiresIn})
	benchmarkStore(store, 10, 100000, b)
}

func BenchmarkRateLimiterMemoryStore_conc100_10000(b *testing.B) {
	var store = NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{Rate: 100, Burst: 200, ExpiresIn: testExpiresIn})
	benchmarkStore(store, 100, 10000, b)
}

// TestRateLimiterMemoryStore_TOCTOUFix verifies that the TOCTOU race condition is fixed
// by ensuring timeNow() is only called once per Allow() call
func TestRateLimiterMemoryStore_TOCTOUFix(t *testing.T) {
	t.Parallel()

	store := NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{
		Rate:      1,
		Burst:     1,
		ExpiresIn: 2 * time.Second,
	})

	// Track time calls to verify we use the same time value
	timeCallCount := 0
	baseTime := time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)

	store.timeNow = func() time.Time {
		timeCallCount++
		return baseTime
	}

	// First request - should succeed
	allowed, err := store.Allow("127.0.0.1")
	assert.NoError(t, err)
	assert.True(t, allowed, "First request should be allowed")

	// Verify timeNow() was only called once
	assert.Equal(t, 1, timeCallCount, "timeNow() should only be called once per Allow()")
}

// TestRateLimiterMemoryStore_ConcurrentAccess verifies rate limiting correctness under concurrent load
func TestRateLimiterMemoryStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	store := NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{
		Rate:      10,
		Burst:     5,
		ExpiresIn: 5 * time.Second,
	})

	const goroutines = 50
	const requestsPerGoroutine = 20

	var wg sync.WaitGroup
	var allowedCount, deniedCount int32

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				allowed, err := store.Allow("test-user")
				assert.NoError(t, err)
				if allowed {
					atomic.AddInt32(&allowedCount, 1)
				} else {
					atomic.AddInt32(&deniedCount, 1)
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	totalRequests := goroutines * requestsPerGoroutine
	allowed := int(allowedCount)
	denied := int(deniedCount)

	assert.Equal(t, totalRequests, allowed+denied, "All requests should be processed")
	assert.Greater(t, denied, 0, "Some requests should be denied due to rate limiting")
	assert.Greater(t, allowed, 0, "Some requests should be allowed")
}

// TestRateLimiterMemoryStore_RaceDetection verifies no data races with high concurrency
// Run with: go test -race ./middleware -run TestRateLimiterMemoryStore_RaceDetection
func TestRateLimiterMemoryStore_RaceDetection(t *testing.T) {
	t.Parallel()

	store := NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{
		Rate:      100,
		Burst:     200,
		ExpiresIn: 1 * time.Second,
	})

	const goroutines = 100
	const requestsPerGoroutine = 100

	var wg sync.WaitGroup
	identifiers := []string{"user1", "user2", "user3", "user4", "user5"}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				identifier := identifiers[routineID%len(identifiers)]
				_, err := store.Allow(identifier)
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()
}

// TestRateLimiterMemoryStore_TimeOrdering verifies time ordering consistency in rate limiting decisions
func TestRateLimiterMemoryStore_TimeOrdering(t *testing.T) {
	t.Parallel()

	store := NewRateLimiterMemoryStoreWithConfig(RateLimiterMemoryStoreConfig{
		Rate:      1,
		Burst:     2,
		ExpiresIn: 5 * time.Second,
	})

	currentTime := time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
	store.timeNow = func() time.Time {
		return currentTime
	}

	// First two requests should succeed (burst=2)
	allowed1, _ := store.Allow("user1")
	assert.True(t, allowed1, "Request 1 should be allowed (burst)")

	allowed2, _ := store.Allow("user1")
	assert.True(t, allowed2, "Request 2 should be allowed (burst)")

	// Third request should be denied
	allowed3, _ := store.Allow("user1")
	assert.False(t, allowed3, "Request 3 should be denied (burst exhausted)")

	// Advance time by 1 second
	currentTime = currentTime.Add(1 * time.Second)

	// Fourth request should succeed
	allowed4, _ := store.Allow("user1")
	assert.True(t, allowed4, "Request 4 should be allowed (1 token available)")
}
