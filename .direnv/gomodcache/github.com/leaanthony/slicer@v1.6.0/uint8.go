// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// Uint8Slicer handles slices of uint8
type Uint8Slicer struct {
	slice []uint8
}

// Uint8 creates a new Uint8Slicer
func Uint8(slice ...[]uint8) *Uint8Slicer {
	if len(slice) > 0 {
		return &Uint8Slicer{slice: slice[0]}
	}
	return &Uint8Slicer{}
}

// Add a uint8 value to the slicer
func (s *Uint8Slicer) Add(value uint8, additional ...uint8) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a uint8 value to the slicer if it does not already exist
func (s *Uint8Slicer) AddUnique(value uint8, additional ...uint8) {

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

// AddSlice adds a uint8 slice to the slicer
func (s *Uint8Slicer) AddSlice(value []uint8) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *Uint8Slicer) AsSlice() []uint8 {
	return s.slice
}

// AddSlicer appends a Uint8Slicer to the slicer
func (s *Uint8Slicer) AddSlicer(value *Uint8Slicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *Uint8Slicer) Filter(fn func(uint8) bool) *Uint8Slicer {
	result := &Uint8Slicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *Uint8Slicer) Each(fn func(uint8)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *Uint8Slicer) Contains(matcher uint8) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *Uint8Slicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *Uint8Slicer) Clear() {
	s.slice = []uint8{}
}

// Deduplicate removes duplicate values from the slice
func (s *Uint8Slicer) Deduplicate() {

	result := &Uint8Slicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *Uint8Slicer) Join(separator string) string {
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
func (s *Uint8Slicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
