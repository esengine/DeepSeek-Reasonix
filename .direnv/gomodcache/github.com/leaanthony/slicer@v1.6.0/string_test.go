package slicer

import (
	"strings"
	"testing"
)

func TestStringFilter(t *testing.T) {

	s := String()
	s.Add("one")
	s.Add("two")
	s.Add("one")
	s.Add("three")
	s.Add("one")

	expected := "one one one"
	filtered := s.Filter(func(v string) bool {
		return v == "one"
	})
	actual := strings.Join(filtered.AsSlice(), " ")
	if expected != actual {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestStringEach(t *testing.T) {

	s := String()
	s.Add("one")
	s.Add("two")
	s.Add("three")

	result := String()
	s.Each(func(elem string) {
		result.Add(elem + "!")
	})
	expected := "one! two! three!"
	actual := strings.Join(result.AsSlice(), " ")
	if expected != actual {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

// TestOptionalStringSlice tests when you construct a String with
// an existing slice
func TestOptionalStringSlice(t *testing.T) {
	data := []string{"one", "two", "three"}
	s := String(data)

	result := ""
	s.Each(func(elem string) {
		result += elem
	})
	expected := "onetwothree"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}

// TestSort tests that the slicer can be sorted
func TestSort(t *testing.T) {
	data := []string{"cat", "bat", "dog", "arachnid"}
	s := String(data)
	s.Sort()
	result := ""
	s.Each(func(elem string) {
		result += elem
	})
	expected := "arachnidbatcatdog"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
