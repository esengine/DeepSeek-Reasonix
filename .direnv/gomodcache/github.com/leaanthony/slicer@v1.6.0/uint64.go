// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// Uint64Slicer handles slices of uint64
type Uint64Slicer struct {
	slice []uint64
}

// Uint64 creates a new Uint64Slicer
func Uint64(slice ...[]uint64) *Uint64Slicer {
	if len(slice) > 0 {
		return &Uint64Slicer{slice: slice[0]}
	}
	return &Uint64Slicer{}
}

// Add a uint64 value to the slicer
func (s *Uint64Slicer) Add(value uint64, additional ...uint64) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a uint64 value to the slicer if it does not already exist
func (s *Uint64Slicer) AddUnique(value uint64, additional ...uint64) {

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

// AddSlice adds a uint64 slice to the slicer
func (s *Uint64Slicer) AddSlice(value []uint64) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *Uint64Slicer) AsSlice() []uint64 {
	return s.slice
}

// AddSlicer appends a Uint64Slicer to the slicer
func (s *Uint64Slicer) AddSlicer(value *Uint64Slicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *Uint64Slicer) Filter(fn func(uint64) bool) *Uint64Slicer {
	result := &Uint64Slicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *Uint64Slicer) Each(fn func(uint64)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *Uint64Slicer) Contains(matcher uint64) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *Uint64Slicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *Uint64Slicer) Clear() {
	s.slice = []uint64{}
}

// Deduplicate removes duplicate values from the slice
func (s *Uint64Slicer) Deduplicate() {

	result := &Uint64Slicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *Uint64Slicer) Join(separator string) string {
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
func (s *Uint64Slicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
