// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// Int8Slicer handles slices of int8
type Int8Slicer struct {
	slice []int8
}

// Int8 creates a new Int8Slicer
func Int8(slice ...[]int8) *Int8Slicer {
	if len(slice) > 0 {
		return &Int8Slicer{slice: slice[0]}
	}
	return &Int8Slicer{}
}

// Add a int8 value to the slicer
func (s *Int8Slicer) Add(value int8, additional ...int8) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a int8 value to the slicer if it does not already exist
func (s *Int8Slicer) AddUnique(value int8, additional ...int8) {

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

// AddSlice adds a int8 slice to the slicer
func (s *Int8Slicer) AddSlice(value []int8) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *Int8Slicer) AsSlice() []int8 {
	return s.slice
}

// AddSlicer appends a Int8Slicer to the slicer
func (s *Int8Slicer) AddSlicer(value *Int8Slicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *Int8Slicer) Filter(fn func(int8) bool) *Int8Slicer {
	result := &Int8Slicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *Int8Slicer) Each(fn func(int8)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *Int8Slicer) Contains(matcher int8) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *Int8Slicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *Int8Slicer) Clear() {
	s.slice = []int8{}
}

// Deduplicate removes duplicate values from the slice
func (s *Int8Slicer) Deduplicate() {

	result := &Int8Slicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *Int8Slicer) Join(separator string) string {
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
func (s *Int8Slicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
