// Package slicer contains utility classes for handling slices
package slicer

// Imports
import "sort"
import "fmt"
import "strings"

// UintSlicer handles slices of uint
type UintSlicer struct {
	slice []uint
}

// Uint creates a new UintSlicer
func Uint(slice ...[]uint) *UintSlicer {
	if len(slice) > 0 {
		return &UintSlicer{slice: slice[0]}
	}
	return &UintSlicer{}
}

// Add a uint value to the slicer
func (s *UintSlicer) Add(value uint, additional ...uint) {
	s.slice = append(s.slice, value)
	s.slice = append(s.slice, additional...)
}

// AddUnique adds a uint value to the slicer if it does not already exist
func (s *UintSlicer) AddUnique(value uint, additional ...uint) {

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

// AddSlice adds a uint slice to the slicer
func (s *UintSlicer) AddSlice(value []uint) {
	s.slice = append(s.slice, value...)
}

// AsSlice returns the slice
func (s *UintSlicer) AsSlice() []uint {
	return s.slice
}

// AddSlicer appends a UintSlicer to the slicer
func (s *UintSlicer) AddSlicer(value *UintSlicer) {
	s.slice = append(s.slice, value.AsSlice()...)
}

// Filter the slice based on the given function
func (s *UintSlicer) Filter(fn func(uint) bool) *UintSlicer {
	result := &UintSlicer{}
	for _, elem := range s.slice {
		if fn(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Each runs a function on every element of the slice
func (s *UintSlicer) Each(fn func(uint)) {
	for _, elem := range s.slice {
		fn(elem)
	}
}

// Contains indicates if the given value is in the slice
func (s *UintSlicer) Contains(matcher uint) bool {
	result := false
	for _, elem := range s.slice {
		if elem == matcher {
			result = true
		}
	}
	return result
}

// Length returns the number of elements in the slice
func (s *UintSlicer) Length() int {
	return len(s.slice)
}

// Clear all elements in the slice
func (s *UintSlicer) Clear() {
	s.slice = []uint{}
}

// Deduplicate removes duplicate values from the slice
func (s *UintSlicer) Deduplicate() {

	result := &UintSlicer{}

	for _, elem := range s.slice {
		if !result.Contains(elem) {
			result.Add(elem)
		}
	}

	s.slice = result.AsSlice()
}

// Join returns a string with the slicer elements separated by the given separator
func (s *UintSlicer) Join(separator string) string {
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
func (s *UintSlicer) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}
