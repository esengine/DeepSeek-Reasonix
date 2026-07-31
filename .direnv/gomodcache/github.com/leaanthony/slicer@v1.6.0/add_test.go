package slicer

import (
	"strings"
	"testing"
)

func TestStringAdd(t *testing.T) {

	s := String()
	s.Add("one")
	s.Add("two")

	expected := "one two"
	actual := strings.Join(s.AsSlice(), " ")
	if expected != actual {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}

	// Add more than one value
	s.Clear()
	s.Add("one", "two")
	actual = strings.Join(s.AsSlice(), " ")
	if expected != actual {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestStringAddUnique(t *testing.T) {

	s := String()
	s.AddUnique("one")
	s.AddUnique("one")
	s.AddUnique("two")
	s.AddUnique("two")

	expected := "one two"
	actual := strings.Join(s.AsSlice(), " ")
	if expected != actual {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}

	// AddUnique more than one value
	s.Clear()
	s.AddUnique("one", "two", "two")
	actual = strings.Join(s.AsSlice(), " ")
	if expected != actual {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestStringAddSlice(t *testing.T) {

	s := String()
	s.Add("one")
	s.Add("two")
	extras := []string{"three", "four"}
	s.AddSlice(extras)

	expected := "one two three four"
	actual := strings.Join(s.AsSlice(), " ")
	if expected != actual {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestStringAddSlicer(t *testing.T) {

	s := String()
	s.Add("one")
	s.Add("two")

	p := String()
	p.Add("three")
	p.Add("four")

	s.AddSlicer(p)

	expected := "one two three four"
	actual := strings.Join(s.AsSlice(), " ")
	if expected != actual {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
