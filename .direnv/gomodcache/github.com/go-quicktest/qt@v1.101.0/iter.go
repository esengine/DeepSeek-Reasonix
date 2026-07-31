// Licensed under the MIT license, see LICENSE file for details.

package qt

import (
	"fmt"
	"reflect"
)

// containerIter provides an interface for iterating over a container
// (map, slice or array).
type containerIter[T any] interface {
	// next advances to the next item in the container.
	next() bool
	// key returns the current key as a string.
	key() string
	// value returns the current value.
	value() T
}

func newSliceIter[T any](slice []T) *sliceIter[T] {
	return &sliceIter[T]{
		slice: slice,
		index: -1,
	}
}

// sliceIter implements containerIter for slices and arrays.
type sliceIter[T any] struct {
	slice []T
	index int
}

func (i *sliceIter[T]) next() bool {
	i.index++
	return i.index < len(i.slice)
}

func (i *sliceIter[T]) value() T {
	return i.slice[i.index]
}

func (i *sliceIter[T]) key() string {
	return fmt.Sprintf("index %d", i.index)
}

func newMapIter[K comparable, V any](m map[K]V) containerIter[V] {
	return mapValueIter[V]{
		iter: reflect.ValueOf(m).MapRange(),
	}
}

// mapValueIter implements containerIter for maps.
type mapValueIter[T any] struct {
	iter *reflect.MapIter
	v    T
}

func (i mapValueIter[T]) next() bool {
	return i.iter.Next()
}

func (i mapValueIter[T]) key() string {
	return fmt.Sprintf("key %#v", i.iter.Key())
}

func (i mapValueIter[T]) value() T {
	return valueAs[T](i.iter.Value())
}
