package sftp

import (
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllocator(t *testing.T) {
	allocator := newAllocator()
	// get a page for request order id 1
	page := allocator.GetPage(1)
	page[1] = uint8(1)
	assert.Equal(t, maxMsgLength, len(page))
	assert.Equal(t, 1, allocator.countUsedPages())
	// get another page for request order id 1, we now have 2 used pages
	page = allocator.GetPage(1)
	page[0] = uint8(2)
	assert.Equal(t, 2, allocator.countUsedPages())
	// get another page for request order id 1, we now have 3 used pages
	page = allocator.GetPage(1)
	page[2] = uint8(3)
	assert.Equal(t, 3, allocator.countUsedPages())
	// release the page for request order id 1, we now have 3 available pages
	allocator.ReleasePages(1)
	assert.NotContains(t, allocator.used, 1)
	assert.Equal(t, 3, allocator.countAvailablePages())
	// get a page for request order id 2
	// we get the latest released page, let's verify that by checking the previously written values
	// so we are sure we are reusing a previously allocated page
	page = allocator.GetPage(2)
	assert.Equal(t, uint8(3), page[2])
	assert.Equal(t, 2, allocator.countAvailablePages())
	assert.Equal(t, 1, allocator.countUsedPages())
	page = allocator.GetPage(2)
	assert.Equal(t, uint8(2), page[0])
	assert.Equal(t, 1, allocator.countAvailablePages())
	assert.Equal(t, 2, allocator.countUsedPages())
	page = allocator.GetPage(2)
	assert.Equal(t, uint8(1), page[1])
	// we now have 3 used pages for request order id 2 and no available pages
	assert.Equal(t, 0, allocator.countAvailablePages())
	assert.Equal(t, 3, allocator.countUsedPages())
	assert.True(t, allocator.isRequestOrderIDUsed(2), "page with request order id 2 must be used")
	assert.False(t, allocator.isRequestOrderIDUsed(1), "page with request order id 1 must be not used")
	// release some request order id with no allocated pages, should have no effect
	allocator.ReleasePages(1)
	allocator.ReleasePages(3)
	assert.Equal(t, 0, allocator.countAvailablePages())
	assert.Equal(t, 3, allocator.countUsedPages())
	assert.True(t, allocator.isRequestOrderIDUsed(2), "page with request order id 2 must be used")
	assert.False(t, allocator.isRequestOrderIDUsed(1), "page with request order id 1 must be not used")
	// now get some pages for another request order id
	allocator.GetPage(3)
	// we now must have 3 used pages for request order id 2 and 1 used page for request order id 3
	assert.Equal(t, 0, allocator.countAvailablePages())
	assert.Equal(t, 4, allocator.countUsedPages())
	assert.True(t, allocator.isRequestOrderIDUsed(2), "page with request order id 2 must be used")
	assert.True(t, allocator.isRequestOrderIDUsed(3), "page with request order id 3 must be used")
	assert.False(t, allocator.isRequestOrderIDUsed(1), "page with request order id 1 must be not used")
	// get another page for request order id 3
	allocator.GetPage(3)
	assert.Equal(t, 0, allocator.countAvailablePages())
	assert.Equal(t, 5, allocator.countUsedPages())
	assert.True(t, allocator.isRequestOrderIDUsed(2), "page with request order id 2 must be used")
	assert.True(t, allocator.isRequestOrderIDUsed(3), "page with request order id 3 must be used")
	assert.False(t, allocator.isRequestOrderIDUsed(1), "page with request order id 1 must be not used")
	// now release the pages for request order id 3
	allocator.ReleasePages(3)
	assert.Equal(t, 2, allocator.countAvailablePages())
	assert.Equal(t, 3, allocator.countUsedPages())
	assert.True(t, allocator.isRequestOrderIDUsed(2), "page with request order id 2 must be used")
	assert.False(t, allocator.isRequestOrderIDUsed(1), "page with request order id 1 must be not used")
	assert.False(t, allocator.isRequestOrderIDUsed(3), "page with request order id 3 must be not used")
	// again check we are reusing previously allocated pages.
	// We have written nothing to the 2 last requested page so release them and get the third one
	allocator.ReleasePages(2)
	assert.Equal(t, 5, allocator.countAvailablePages())
	assert.Equal(t, 0, allocator.countUsedPages())
	assert.False(t, allocator.isRequestOrderIDUsed(2), "page with request order id 2 must be not used")
	allocator.GetPage(4)
	allocator.GetPage(4)
	page = allocator.GetPage(4)
	assert.Equal(t, uint8(3), page[2])
	assert.Equal(t, 2, allocator.countAvailablePages())
	assert.Equal(t, 3, allocator.countUsedPages())
	assert.True(t, allocator.isRequestOrderIDUsed(4), "page with request order id 4 must be used")
	// free the allocator
	allocator.Free()
	assert.Equal(t, 0, allocator.countAvailablePages())
	assert.Equal(t, 0, allocator.countUsedPages())
}

func BenchmarkAllocatorSerial(b *testing.B) {
	allocator := newAllocator()
	for i := 0; i < b.N; i++ {
		benchAllocator(allocator, uint32(i))
	}
}

func BenchmarkAllocatorParallel(b *testing.B) {
	var counter uint32
	allocator := newAllocator()
	for i := 1; i <= 8; i *= 2 {
		b.Run(strconv.Itoa(i), func(b *testing.B) {
			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					benchAllocator(allocator, atomic.AddUint32(&counter, 1))
				}
			})
		})
	}
}

func benchAllocator(allocator *allocator, requestOrderID uint32) {
	// simulates the page requested in recvPacket
	allocator.GetPage(requestOrderID)
	// simulates the page requested in fileget for downloads
	allocator.GetPage(requestOrderID)
	// release the allocated pages
	allocator.ReleasePages(requestOrderID)
}

// useful for debug
func printAllocatorContents(allocator *allocator) {
	for o, u := range allocator.used {
		debug("used order id: %v, values: %+v", o, u)
	}
	for _, v := range allocator.available {
		debug("available, values: %+v", v)
	}
}
