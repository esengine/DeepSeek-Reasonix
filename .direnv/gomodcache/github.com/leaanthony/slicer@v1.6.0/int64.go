// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// Int64Slicer handles slices of int64
type Int64Slicer struct {
	slice []int64
}

// Int64 creates a new Int64Slicer
func Int64(slice ...[]int64) *Int64Slicer {
	if len(slice) > 0 {
		return &Int64Slicer{slice: slice[0]}
	}
	return &Int64Slicer{}
}

// Add a int64 value to the slicer
func (s *Int64Slicer) Add(value int64, additional ...int64) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a int64 value to the slicer if it does not already exist
func (s *Int64Slicer) AddUnique(value int64, additional ...int64) {

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

// AddSlice adds a int64 slice to the slicer
func (s *Int64Slicer) AddSlice(value []int64) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *Int64Slicer) AsSlice() []int64 {
	return s.slice
}

// AddSlicer appends a Int64Slicer to the slicer
func (s *Int64Slicer) AddSlicer(value *Int64Slicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *Int64Slicer) Filter(fn func(int64) bool) *Int64Slicer {
	result := &Int64Slicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *Int64Slicer) Each(fn func(int64)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *Int64Slicer) Contains(matcher int64) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *Int64Slicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *Int64Slicer) Clear() {
	s.slice = []int64{}
}

// Deduplicate removes duplicate values from the slice
func (s *Int64Slicer) Deduplicate() {

	result := &Int64Slicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *Int64Slicer) Join(separator string) string {
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
func (s *Int64Slicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
