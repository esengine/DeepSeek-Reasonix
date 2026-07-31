// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// Int32Slicer handles slices of int32
type Int32Slicer struct {
	slice []int32
}

// Int32 creates a new Int32Slicer
func Int32(slice ...[]int32) *Int32Slicer {
	if len(slice) > 0 {
		return &Int32Slicer{slice: slice[0]}
	}
	return &Int32Slicer{}
}

// Add a int32 value to the slicer
func (s *Int32Slicer) Add(value int32, additional ...int32) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a int32 value to the slicer if it does not already exist
func (s *Int32Slicer) AddUnique(value int32, additional ...int32) {

	if !s.Contains(value) {
		s.slice = append(s.slice, value)
	}

	// Add additional values
	for _, value := range additional {
		if !s.Contains(value) {
			s.slice = append(s.slice, value)
		}
	}
}

// AddSlice adds a int32 slice to the slicer
func (s *Int32Slicer) AddSlice(value []int32) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *Int32Slicer) AsSlice() []int32 {
	return s.slice
}

// AddSlicer appends a Int32Slicer to the slicer
func (s *Int32Slicer) AddSlicer(value *Int32Slicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *Int32Slicer) Filter(fn func(int32) bool) *Int32Slicer {
	result := &Int32Slicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *Int32Slicer) Each(fn func(int32)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *Int32Slicer) Contains(matcher int32) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *Int32Slicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *Int32Slicer) Clear() {
	s.slice = []int32{}
}

// Deduplicate removes duplicate values from the slice
func (s *Int32Slicer) Deduplicate() {

	result := &Int32Slicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *Int32Slicer) Join(separator string) string {
	var builder strings.Builder

	// Shortcut no elements
	if len(s.slice) == 0 {
		return ""
	}

	// Iterate over length - 1
	index := 0
	for index = 0; index < len(s.slice)-1; index++ {
		builder.WriteString(fmt.Sprintf("%v%s", s.slice[index], separator))
	}
	builder.WriteString(fmt.Sprintf("%v", s.slice[index]))
	result := builder.String()
	return result
}

// Sort the slice values
func (s *Int32Slicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
