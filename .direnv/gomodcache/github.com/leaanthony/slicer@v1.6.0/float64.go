// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// Float64Slicer handles slices of float64
type Float64Slicer struct {
	slice []float64
}

// Float64 creates a new Float64Slicer
func Float64(slice ...[]float64) *Float64Slicer {
	if len(slice) > 0 {
		return &Float64Slicer{slice: slice[0]}
	}
	return &Float64Slicer{}
}

// Add a float64 value to the slicer
func (s *Float64Slicer) Add(value float64, additional ...float64) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a float64 value to the slicer if it does not already exist
func (s *Float64Slicer) AddUnique(value float64, additional ...float64) {

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

// AddSlice adds a float64 slice to the slicer
func (s *Float64Slicer) AddSlice(value []float64) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *Float64Slicer) AsSlice() []float64 {
	return s.slice
}

// AddSlicer appends a Float64Slicer to the slicer
func (s *Float64Slicer) AddSlicer(value *Float64Slicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *Float64Slicer) Filter(fn func(float64) bool) *Float64Slicer {
	result := &Float64Slicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *Float64Slicer) Each(fn func(float64)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *Float64Slicer) Contains(matcher float64) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *Float64Slicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *Float64Slicer) Clear() {
	s.slice = []float64{}
}

// Deduplicate removes duplicate values from the slice
func (s *Float64Slicer) Deduplicate() {

	result := &Float64Slicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *Float64Slicer) Join(separator string) string {
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
func (s *Float64Slicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
