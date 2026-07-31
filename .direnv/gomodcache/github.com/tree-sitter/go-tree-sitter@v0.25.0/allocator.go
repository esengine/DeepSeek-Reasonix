package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>
#include "allocator.h"
*/
import "C"

import (
	"sync/atomic"
	"unsafe"
)

var (
	malloc_fn  atomic.Value
	calloc_fn  atomic.Value
	realloc_fn atomic.Value
	free_fn    atomic.Value
)

func init() {
	malloc_fn.Store(func(size C.size_t) unsafe.Pointer {
		return C.malloc(size)
	})
	calloc_fn.Store(func(num, size C.size_t) unsafe.Pointer {
		return C.calloc(num, size)
	})
	realloc_fn.Store(func(ptr unsafe.Pointer, size C.size_t) unsafe.Pointer {
		return C.realloc(ptr, size)
	})
	free_fn.Store(func(ptr unsafe.Pointer) {
		C.free(ptr)
	})
	SetAllocator(nil, nil, nil, nil)
}

//export go_malloc
func go_malloc(size C.size_t) unsafe.Pointer {
	return malloc_fn.Load().(func(C.size_t) unsafe.Pointer)(size)
}

//export go_calloc
func go_calloc(num, size C.size_t) unsafe.Pointer {
	return calloc_fn.Load().(func(C.size_t, C.size_t) unsafe.Pointer)(num, size)
}

//export go_realloc
func go_realloc(ptr unsafe.Pointer, size C.size_t) unsafe.Pointer {
	return realloc_fn.Load().(func(unsafe.Pointer, C.size_t) unsafe.Pointer)(ptr, size)
}

//export go_free
func go_free(ptr unsafe.Pointer) {
	free_fn.Load().(func(unsafe.Pointer))(ptr)
}

// Sets the memory allocation functions that the core library should use.
func SetAllocator(
	newMalloc func(size uint) unsafe.Pointer,
	newCalloc func(num, size uint) unsafe.Pointer,
	newRealloc func(ptr unsafe.Pointer, size uint) unsafe.Pointer,
	newFree func(ptr unsafe.Pointer),
) {
	if newMalloc != nil {
		malloc_fn.Store(func(size C.size_t) unsafe.Pointer {
			return newMalloc(uint(size))
		})
	} else {
		malloc_fn.Store(func(size C.size_t) unsafe.Pointer {
			return C.malloc(size)
		})
	}

	if newCalloc != nil {
		calloc_fn.Store(func(num, size C.size_t) unsafe.Pointer {
			return newCalloc(uint(num), uint(size))
		})
	} else {
		calloc_fn.Store(func(num, size C.size_t) unsafe.Pointer {
			return C.calloc(num, size)
		})
	}

	if newRealloc != nil {
		realloc_fn.Store(func(ptr unsafe.Pointer, size C.size_t) unsafe.Pointer {
			return newRealloc(ptr, uint(size))
		})
	} else {
		realloc_fn.Store(func(ptr unsafe.Pointer, size C.size_t) unsafe.Pointer {
			return C.realloc(ptr, size)
		})
	}

	if newFree != nil {
		free_fn.Store(func(ptr unsafe.Pointer) {
			newFree(ptr)
		})
	} else {
		free_fn.Store(func(ptr unsafe.Pointer) {
			C.free(ptr)
		})
	}

	C.ts_set_allocator(
		(*[0]byte)(C.c_malloc_fn),
		(*[0]byte)(C.c_calloc_fn),
		(*[0]byte)(C.c_realloc_fn),
		(*[0]byte)(C.c_free_fn),
	)
}
