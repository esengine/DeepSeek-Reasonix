// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// Float32Slicer handles slices of float32
type Float32Slicer struct {
	slice []float32
}

// Float32 creates a new Float32Slicer
func Float32(slice ...[]float32) *Float32Slicer {
	if len(slice) > 0 {
		return &Float32Slicer{slice: slice[0]}
	}
	return &Float32Slicer{}
}

// Add a float32 value to the slicer
func (s *Float32Slicer) Add(value float32, additional ...float32) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a float32 value to the slicer if it does not already exist
func (s *Float32Slicer) AddUnique(value float32, additional ...float32) {

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

// AddSlice adds a float32 slice to the slicer
func (s *Float32Slicer) AddSlice(value []float32) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *Float32Slicer) AsSlice() []float32 {
	return s.slice
}

// AddSlicer appends a Float32Slicer to the slicer
func (s *Float32Slicer) AddSlicer(value *Float32Slicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *Float32Slicer) Filter(fn func(float32) bool) *Float32Slicer {
	result := &Float32Slicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *Float32Slicer) Each(fn func(float32)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *Float32Slicer) Contains(matcher float32) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *Float32Slicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *Float32Slicer) Clear() {
	s.slice = []float32{}
}

// Deduplicate removes duplicate values from the slice
func (s *Float32Slicer) Deduplicate() {

	result := &Float32Slicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *Float32Slicer) Join(separator string) string {
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
func (s *Float32Slicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
