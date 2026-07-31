// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// Uint32Slicer handles slices of uint32
type Uint32Slicer struct {
	slice []uint32
}

// Uint32 creates a new Uint32Slicer
func Uint32(slice ...[]uint32) *Uint32Slicer {
	if len(slice) > 0 {
		return &Uint32Slicer{slice: slice[0]}
	}
	return &Uint32Slicer{}
}

// Add a uint32 value to the slicer
func (s *Uint32Slicer) Add(value uint32, additional ...uint32) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a uint32 value to the slicer if it does not already exist
func (s *Uint32Slicer) AddUnique(value uint32, additional ...uint32) {

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

// AddSlice adds a uint32 slice to the slicer
func (s *Uint32Slicer) AddSlice(value []uint32) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *Uint32Slicer) AsSlice() []uint32 {
	return s.slice
}

// AddSlicer appends a Uint32Slicer to the slicer
func (s *Uint32Slicer) AddSlicer(value *Uint32Slicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *Uint32Slicer) Filter(fn func(uint32) bool) *Uint32Slicer {
	result := &Uint32Slicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *Uint32Slicer) Each(fn func(uint32)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *Uint32Slicer) Contains(matcher uint32) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *Uint32Slicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *Uint32Slicer) Clear() {
	s.slice = []uint32{}
}

// Deduplicate removes duplicate values from the slice
func (s *Uint32Slicer) Deduplicate() {

	result := &Uint32Slicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *Uint32Slicer) Join(separator string) string {
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
func (s *Uint32Slicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
