package slicer

import (
	"github.com/matryer/is"
	"testing"
)

func TestUint8Add(t *testing.T) {

	is2 := is.New(t)
	s := Uint8()
	s.Add(1)
	s.Add(2)

	is2.Equal(s.AsSlice(), []uint8{1, 2})

	s.Clear()
	s.Add(1, 2)

	is2.Equal(s.AsSlice(), []uint8{1, 2})

}

func TestUint8Length(t *testing.T) {
	is2 := is.New(t)
	s := Uint8()
	s.Add(1)
	s.Add(2)

	is2.Equal(s.Length(), 2)
}

func TestUint8AddUnique(t *testing.T) {
	is2 := is.New(t)
	s := Uint8()
	s.AddUnique(1)
	s.AddUnique(2)
	s.AddUnique(2)
	s.AddUnique(2)

	is2.Equal(s.AsSlice(), []uint8{1, 2})

	s.Clear()
	s.AddUnique(1, 2, 1, 2, 2, 1)

	is2.Equal(s.AsSlice(), []uint8{1, 2})
}

func TestUint8Deduplicate(t *testing.T) {
	is2 := is.New(t)
	s := Uint8()
	s.Add(1)
	s.Add(2)
	s.Add(2)
	s.Add(2)

	is2.Equal(s.Length(), 4)
	is2.Equal(s.AsSlice(), []uint8{1, 2, 2, 2})
	s.Deduplicate()
	is2.Equal(s.Length(), 2)
	is2.Equal(s.AsSlice(), []uint8{1, 2})
}

func TestUint8AddSlice(t *testing.T) {
	is2 := is.New(t)

	s := Uint8()
	s.Add(1)
	s.Add(2)

	extras := []uint8{3, 4}

	s.AddSlice(extras)
	is2.Equal(s.AsSlice(), []uint8{1, 2, 3, 4})
}

func TestUint8AddSlicer(t *testing.T) {

	is2 := is.New(t)

	s := Uint8()
	s.Add(1)
	s.Add(2)

	p := Uint8()
	p.Add(3)
	p.Add(4)

	s.AddSlicer(p)

	is2.Equal(s.AsSlice(), []uint8{1, 2, 3, 4})
}

func TestUint8Filter(t *testing.T) {

	is2 := is.New(t)
	s := Uint8()
	s.Add(18)
	s.Add(120)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	result := s.Filter(func(i uint8) bool {
		return i > 19
	})

	is2.Equal(result.AsSlice(), []uint8{120, 20, 29})

}

func TestUint8Each(t *testing.T) {

	is2 := is.New(t)
	s := Uint8()
	s.Add(18)
	s.Add(10)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	var result uint8

	s.Each(func(i uint8) {
		result = result + i
	})
	var expected uint8 = 91
	is2.Equal(result, expected)
}

// TestOptionalUint8Slice tests when you construct a Uint8 with
// an existing slice
func TestOptionalUint8Slice(t *testing.T) {
	is2 := is.New(t)

	data := []uint8{1, 2, 3}
	s := Uint8(data)

	var result uint8 = 0
	s.Each(func(elem uint8) {
		result += elem
	})
	var expected uint8 = 6
	is2.Equal(result, expected)

}

// TestUint8Sort tests that the slicer can be sorted
func TestUint8Sort(t *testing.T) {
	is2 := is.New(t)

	data := []uint8{5, 4, 3, 2, 1}
	s := Uint8(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	is2.Equal(result, expected)
	s.Clear()
	is2.Equal(s.Join(""), "")
}
