// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "fmt"
import "strings"

// BoolSlicer handles slices of bool
type BoolSlicer struct {
	slice []bool
}

// Bool creates a new BoolSlicer
func Bool(slice ...[]bool) *BoolSlicer {
	if len(slice) > 0 {
		return &BoolSlicer{slice: slice[0]}
	}
	return &BoolSlicer{}
}

// Add a bool value to the slicer
func (s *BoolSlicer) Add(value bool, additional ...bool) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a bool value to the slicer if it does not already exist
func (s *BoolSlicer) AddUnique(value bool, additional ...bool) {

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

// AddSlice adds a bool slice to the slicer
func (s *BoolSlicer) AddSlice(value []bool) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *BoolSlicer) AsSlice() []bool {
	return s.slice
}

// AddSlicer appends a BoolSlicer to the slicer
func (s *BoolSlicer) AddSlicer(value *BoolSlicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *BoolSlicer) Filter(fn func(bool) bool) *BoolSlicer {
	result := &BoolSlicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *BoolSlicer) Each(fn func(bool)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *BoolSlicer) Contains(matcher bool) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *BoolSlicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *BoolSlicer) Clear() {
	s.slice = []bool{}
}

// Deduplicate removes duplicate values from the slice
func (s *BoolSlicer) Deduplicate() {

	result := &BoolSlicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *BoolSlicer) Join(separator string) string {
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
