// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// Uint16Slicer handles slices of uint16
type Uint16Slicer struct {
	slice []uint16
}

// Uint16 creates a new Uint16Slicer
func Uint16(slice ...[]uint16) *Uint16Slicer {
	if len(slice) > 0 {
		return &Uint16Slicer{slice: slice[0]}
	}
	return &Uint16Slicer{}
}

// Add a uint16 value to the slicer
func (s *Uint16Slicer) Add(value uint16, additional ...uint16) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a uint16 value to the slicer if it does not already exist
func (s *Uint16Slicer) AddUnique(value uint16, additional ...uint16) {

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

// AddSlice adds a uint16 slice to the slicer
func (s *Uint16Slicer) AddSlice(value []uint16) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *Uint16Slicer) AsSlice() []uint16 {
	return s.slice
}

// AddSlicer appends a Uint16Slicer to the slicer
func (s *Uint16Slicer) AddSlicer(value *Uint16Slicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *Uint16Slicer) Filter(fn func(uint16) bool) *Uint16Slicer {
	result := &Uint16Slicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *Uint16Slicer) Each(fn func(uint16)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *Uint16Slicer) Contains(matcher uint16) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *Uint16Slicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *Uint16Slicer) Clear() {
	s.slice = []uint16{}
}

// Deduplicate removes duplicate values from the slice
func (s *Uint16Slicer) Deduplicate() {

	result := &Uint16Slicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *Uint16Slicer) Join(separator string) string {
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
func (s *Uint16Slicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
